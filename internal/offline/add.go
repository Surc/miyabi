package offline

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/domain/download"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/offlinedownload"
	"github.com/ppxb/miyabi/internal/javdb"
)

// Add submits an offline download to the first mounted 115 account. It is the
// default entry for automation without an explicit account; the API passes an
// account explicitly through AddForAccount.
func (service *Service) Add(ctx context.Context, movieID, hash string) (domain.OfflineSubmission, error) {
	return service.AddByCode(ctx, movieID, "", hash)
}

// AddByCode submits a magnet for a movie whose catalogue code is already known,
// so validating the magnet and recording the movie never re-fetches JavDB
// detail. An empty code keeps the movie-ID lookup path.
func (service *Service) AddByCode(ctx context.Context, movieID, code, hash string) (domain.OfflineSubmission, error) {
	accountID, err := service.defaultAccount(ctx)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}
	return service.addForAccount(ctx, accountID, movieID, code, hash)
}

func (service *Service) defaultAccount(ctx context.Context) (string, error) {
	sources, err := service.drive.Mounts(ctx)
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", domain.E(domain.KindInvalid, "没有已挂载的 115 账号", nil)
	}
	return sources[0].AccountID, nil
}

// AddForAccount validates and records a new offline download for the given 115
// account's mounted directory.
func (service *Service) AddForAccount(ctx context.Context, accountID, movieID, hash string) (domain.OfflineSubmission, error) {
	return service.addForAccount(ctx, accountID, movieID, "", hash)
}

// resolveMagnet confirms the hash belongs to the movie and returns its code.
// A known code skips the movie-ID lookup entirely.
func (service *Service) resolveMagnet(ctx context.Context, movieID, codeHint, hash string) (string, string, error) {
	code := codeid.Normalize(movieID)
	if codeHint != "" {
		code = codeid.Normalize(codeHint)
	}
	if codeHint == "" && !codeid.Valid(code) && javdb.ValidMovieID(movieID) {
		has, err := service.catalogue.HasMagnet(ctx, movieID, hash)
		if err != nil {
			return "", "", err
		}
		if !has {
			return "", "", ErrMagnetNotFound
		}
		code, err = service.catalogue.MovieCode(ctx, movieID)
		return codeid.Normalize(code), movieID, err
	}
	magnets, err := service.catalogue.CatalogueMagnetsByCode(ctx, code)
	if err != nil {
		return "", "", err
	}
	for _, magnet := range magnets {
		if strings.EqualFold(magnet.Hash, hash) {
			return code, "", nil
		}
	}
	return "", "", ErrMagnetNotFound
}

// warmMetadata resolves a code's metadata in the background to populate the
// shared scrape cache. Failures are ignored: the later scrape will retry.
func (service *Service) warmMetadata(code string) {
	if service.metadata == nil || code == "" {
		return
	}
	code = codeid.Normalize(code)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = service.metadata.Resolve(ctx, domain.MovieRef{Code: code})
	}()
}

func (service *Service) addForAccount(ctx context.Context, accountID, movieID, codeHint, hash string) (domain.OfflineSubmission, error) {
	hash = strings.ToLower(hash)
	code, javdbID, err := service.resolveMagnet(ctx, movieID, codeHint, hash)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}
	return service.submitForAccount(ctx, accountID, code, javdbID, hash, "")
}

func (service *Service) AddED2K(ctx context.Context, movieID, resourceID string, page int) (domain.OfflineSubmission, error) {
	return service.AddED2KForAccount(ctx, "", movieID, resourceID, page)
}

func (service *Service) AddED2KForAccount(ctx context.Context, accountID, movieID, resourceID string, page int) (domain.OfflineSubmission, error) {
	resources, err := service.catalogue.ED2KResources(ctx, movieID, page)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}
	for _, resource := range resources.Items {
		if resource.ID == resourceID {
			if accountID == "" {
				accountID, err = service.defaultAccount(ctx)
				if err != nil {
					return domain.OfflineSubmission{}, err
				}
			}
			code, err := service.catalogue.MovieCode(ctx, movieID)
			if err != nil {
				return domain.OfflineSubmission{}, err
			}
			return service.submitForAccount(ctx, accountID, codeid.Normalize(code), movieID, resource.ID, ed2kDownloadURI(resource))
		}
	}
	return domain.OfflineSubmission{}, domain.E(domain.KindInvalid, "ED2K 资源已变化，请刷新后重试", nil)
}

func ed2kDownloadURI(resource domain.ED2KResource) string {
	name, marked := strings.CutSuffix(resource.Name, " LTX")
	if resource.Source != domain.ED2KSourceJavDB || !marked || !domain.IsVideo(name) {
		return resource.URI
	}
	// JavDB can append its LTX label after the extension in the file link.
	parts := strings.SplitN(resource.URI, "|", 4)
	parts[2] = url.PathEscape(name)
	return strings.Join(parts, "|")
}

func (service *Service) submitForAccount(ctx context.Context, accountID, code, javdbID, hash, ed2kURI string) (domain.OfflineSubmission, error) {
	_, prefs, err := service.config(ctx)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}

	if accountID == "" {
		return domain.OfflineSubmission{}, domain.E(domain.KindInvalid, "请选择 115 账号", nil)
	}
	source, ok, err := service.drive.Mount(ctx, accountID)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}
	if !ok {
		return domain.OfflineSubmission{}, domain.E(domain.KindInvalid, "所选 115 账号未挂载媒体目录", nil)
	}
	sess, err := service.drive.OpenSource(ctx, source)
	if err != nil {
		return domain.OfflineSubmission{}, fmt.Errorf("open 115 account for offline download: %w", err)
	}
	directory := source.Directory

	unlock, err := service.operations.Lock(ctx, source.AccountID, hash)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}
	defer unlock()

	existing, err := service.database.OfflineDownload.Query().Where(
		offlinedownload.AccountIDEQ(source.AccountID), candidateHash(hash),
		offlinedownload.StatusEQ(offlinedownload.StatusRunning)).First(ctx)
	if err == nil {
		if existing.DirectoryID != directory.ID {
			return domain.OfflineSubmission{}, domain.E(domain.KindConflict, "该资源正在下载到另一个目录，请先在 115 中处理该任务", nil)
		}
		return service.submission(ctx, existing, &source)
	}
	if !ent.IsNotFound(err) {
		return domain.OfflineSubmission{}, fmt.Errorf("find active offline task: %w", err)
	}

	previous, err := service.database.OfflineDownload.Query().Where(
		offlinedownload.AccountIDEQ(source.AccountID), offlinedownload.DirectoryIDEQ(directory.ID),
		candidateHash(hash), offlinedownload.StatusEQ(offlinedownload.StatusDone)).
		Order(ent.Desc(offlinedownload.FieldID)).First(ctx)
	if err == nil {
		state, err := service.submission(ctx, previous, &source)
		if err != nil {
			return domain.OfflineSubmission{}, err
		}
		if state.Processing {
			return state, nil
		}
	} else if !ent.IsNotFound(err) {
		return domain.OfflineSubmission{}, fmt.Errorf("find download workflow: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return domain.OfflineSubmission{}, err
	}
	done, ok := service.drive.StartWork()
	if !ok {
		return domain.OfflineSubmission{}, context.Canceled
	}
	defer done()

	// Once a remote mutation starts, finish recording it even if the tab closes.
	submitContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.submitTimeout)
	defer cancel()
	state := &download.Recovery{Preferences: prefs, CurrentHash: hash, Action: actionSubmit, SubmissionStarted: true,
		ED2KURI: ed2kURI, Attempts: []download.Attempt{{Hash: hash, StartedAt: service.now()}}}
	infoHash := hash
	if ed2kURI != "" {
		infoHash = ""
	}
	var created *ent.OfflineDownload
	if err := sess.Commit(submitContext, func(tx *ent.Tx) error {
		var err error
		created, err = tx.OfflineDownload.Create().
			SetCode(code).SetJavdbID(javdbID).SetHash(hash).SetInfoHash(infoHash).SetRecovery(state).
			SetAccountID(source.AccountID).SetDirectoryID(directory.ID).
			Save(submitContext)
		return err
	}); err != nil {
		return domain.OfflineSubmission{}, fmt.Errorf("record 115 offline download: %w", err)
	}
	remote, err := service.submit(submitContext, sess, hash, ed2kURI)
	if err != nil {
		return domain.OfflineSubmission{}, service.deferAction(submitContext, created, state, fmt.Errorf("submit 115 offline download: %w", err))
	}
	if err := service.recordSubmission(submitContext, sess, created, state, remote); err != nil {
		return domain.OfflineSubmission{}, service.deferAction(submitContext, created, state, err)
	}

	// Prewarm the metadata cache only after a successful submission, so failed
	// attempts do not trigger a wasted metadata scrape.
	service.warmMetadata(code)
	service.tasks.NotifyOfflineChanged()
	created, err = service.database.OfflineDownload.Get(submitContext, created.ID)
	if err != nil {
		return domain.OfflineSubmission{}, err
	}
	return service.submission(submitContext, created, &source)
}
