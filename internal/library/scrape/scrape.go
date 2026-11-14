package scrape

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/export"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

// MetadataPayload describes the input for a movie scrape job.
type MetadataPayload struct {
	Rebuild         bool                 `json:"rebuild,omitempty"`
	Source          domain.LibrarySource `json:"source"`
	ScanTaskID      int                  `json:"scan_task_id"`
	MovieID         int                  `json:"movie_id"`
	Code            string               `json:"code"`
	JavDBID         string               `json:"javdb_id,omitempty"`
	SelectedJavDBID string               `json:"selected_javdb_id,omitempty"`
	ManualCode      string               `json:"manual_code,omitempty"`
}

// Payload holds the checkpoints of one movie scraping workflow.
type Payload struct {
	MetadataPayload
	PublicationID     string              `json:"publication_id,omitempty"`
	PublicationDigest string              `json:"publication_digest,omitempty"`
	AttemptID         string              `json:"attempt_id,omitempty"`
	MetadataReady     bool                `json:"metadata_ready,omitempty"`
	Document          nfo.Movie           `json:"document"`
	Artwork           *mediaimage.Artwork `json:"artwork,omitempty"`
	PosterVersion     int                 `json:"poster_version,omitempty"`
	Completed         bool                `json:"completed,omitempty"`
}

// MetadataSource resolves independent sources and downloads their image candidates.
type MetadataSource interface {
	Resolve(context.Context, domain.MovieRef) (domain.MovieMetadata, error)
	Fallback(context.Context, domain.MovieRef) (domain.MovieMetadata, error)
	Image(context.Context, domain.ImageCandidate) (domain.Media, error)
}

// Notifier reports committed library updates and queued follow-up work.
type Notifier interface {
	NotifyLibraryChanged()
	WakePool()
}

// SubtitleExporter writes a movie's subtitles beside its exported .strm file.
type SubtitleExporter interface {
	Export(ctx context.Context, movieID int, target subtitlemeta.Target) (int, error)
}

const (
	defaultDirCacheTTL = 45 * time.Second
	maxDirCacheEntries = 128
	maxDirCacheFiles   = 20_000
)

type dirCacheEntry struct {
	files     []pan.File
	expiresAt time.Time
}

// Service manages movie metadata scraping and artwork caching.
type Service struct {
	db        *ent.Client
	drive     *drive.Drive
	metadata  MetadataSource
	images    *mediaimage.Cache
	notifier  Notifier
	subtitles SubtitleExporter

	dirMu    sync.Mutex
	dirCache map[string]dirCacheEntry

	exportMgr     *export.Manager
	mediaNotifier export.MediaNotifier

	artworkDecorator ArtworkDecorator
	beginAttempt     func(context.Context, string) (context.Context, string, error)
}

func (service *Service) exportConfig() export.Config {
	if service.exportMgr != nil {
		return service.exportMgr.Config()
	}
	return export.Config{}
}

type Dependencies struct {
	// BeginAttempt pins optional extension configuration for one recoverable attempt.
	BeginAttempt     func(context.Context, string) (context.Context, string, error)
	ArtworkDecorator ArtworkDecorator

	ExportManager *export.Manager
	MediaNotifier export.MediaNotifier
	Subtitles     SubtitleExporter
}

// New installs dependencies; the application task pools own background workers.
func New(db *ent.Client, d *drive.Drive, metadata MetadataSource, images *mediaimage.Cache, notifier Notifier, deps Dependencies) *Service {
	service := &Service{
		artworkDecorator: deps.ArtworkDecorator,
		beginAttempt:     deps.BeginAttempt,

		db:            db,
		drive:         d,
		metadata:      metadata,
		images:        images,
		notifier:      notifier,
		dirCache:      make(map[string]dirCacheEntry),
		exportMgr:     deps.ExportManager,
		mediaNotifier: deps.MediaNotifier,
		subtitles:     deps.Subtitles,
	}
	return service
}

// Close is retained for existing owners. The application task pools now own
// subtitle worker lifetimes, so this service has no private goroutines to stop.
func (service *Service) Close() {}

// TryLockArtwork attempts to acquire the artwork lock for cache maintenance.
func (service *Service) TryLockArtwork() bool {
	return service.images.TryLockArtwork()
}

// UnlockArtwork releases the artwork lock.
func (service *Service) UnlockArtwork() {
	service.images.UnlockArtwork()
}

// Artwork reads cached artwork bytes by key.
func (service *Service) Artwork(key string) ([]byte, error) {
	return service.images.Read(key)
}

func (service *Service) begin(ctx context.Context, input MetadataPayload) (drive.Session, error) {
	if service.drive == nil {
		return nil, domain.E(domain.KindInvalid, "网盘服务未初始化", nil)
	}
	return service.drive.OpenSource(ctx, input.Source)
}

// Scrape runs one recoverable movie workflow: metadata, artwork, then export.
func (service *Service) Scrape(ctx context.Context, job tasks.Job) error {
	input, err := tasks.DecodePayload[Payload](job.Payload)
	if err != nil {
		return err
	}
	// Recovery precedes replay and all new checkpoints. Never overwrite a
	// publication receipt while its files remain undecided.
	if err := service.exportMgr.WithConfig(func(export.Config) error { return nil }); err != nil {
		return domain.E(domain.KindConflict, "导出发布等待恢复，暂不继续刮削", err)
	}
	if input.Completed {
		return nil
	}
	// An explicit JavDB entry uses its own metadata and artwork.
	if service.beginAttempt != nil && input.SelectedJavDBID == "" {
		// Old checkpoints have no evidence tying their candidates to a configuration.
		// Never combine those candidates with the current bridge settings.
		if input.MetadataReady && input.AttemptID == "" {
			return domain.E(domain.KindConflict, "旧刮削检查点缺少配置快照，请重新刮削", nil)
		}
		ctx, input.AttemptID, err = service.beginAttempt(ctx, input.AttemptID)
		if err != nil {
			return err
		}
	}
	input.Code = codeid.Normalize(input.Code)
	sess, err := service.begin(ctx, input.MetadataPayload)
	if err != nil {
		return err
	}
	current, err := service.db.Movie.Query().Where(movie.IDEQ(input.MovieID)).Select(movie.FieldID, movie.FieldManualCode).Only(ctx)
	if err != nil {
		return err
	}
	if current.ManualCode != input.ManualCode {
		return domain.E(domain.KindConflict, "影片番号已纠正，请使用新的刮削任务", nil)
	}
	if !input.MetadataReady {
		if err := service.prepareMetadata(ctx, sess, job.ID, &input); err != nil {
			return err
		}
	}
	if err := tasks.Checkpoint(ctx, service.db); err != nil {
		return err
	}
	if err := service.prepareArtwork(ctx, job.ID, &input); err != nil {
		return err
	}
	if err := tasks.Checkpoint(ctx, service.db); err != nil {
		return err
	}
	_, err = service.publishMovie(ctx, sess, job, input)
	if err != nil {
		return err
	}
	return nil
}

func (service *Service) prepareMetadata(ctx context.Context, sess drive.Session, taskID int, input *Payload) error {
	record, err := service.db.Movie.Query().Where(movie.IDEQ(input.MovieID),
		movie.HasFilesWith(database.LibraryFiles(input.Source))).Only(ctx)
	if err != nil {
		return fmt.Errorf("load indexed movie for metadata: %w", err)
	}
	if record.ScrapeStatus == movie.ScrapeStatusDone && !input.Rebuild {
		input.Document, err = MovieNFO(record)
		if err != nil {
			return err
		}
		artwork := MovieArtwork(record)
		cached, err := service.images.Exists(artwork)
		if err != nil {
			return err
		}
		if cached {
			input.Artwork = &artwork
		}
		if record.MetadataSnapshot != nil {
			input.PosterVersion = record.MetadataSnapshot.PosterVersion
		}
	} else {
		if id := domain.ValueOrZero(record.JavdbID); id != "" {
			input.JavDBID = id
		}
		if err := service.resolveMetadata(ctx, input); err != nil {
			return err
		}
		if input.SelectedJavDBID == "" || input.SelectedJavDBID == domain.ValueOrZero(record.JavdbID) {
			preserveExtensionMetadata(&input.Document, record.Metadata)
		}
	}
	input.Code = codeid.Normalize(input.Document.Code)
	input.Document.Code = input.Code
	input.MetadataReady = true
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return err
	}
	if err := sess.Commit(ctx, func(tx *ent.Tx) error {
		// Candidates belong to the task until publication succeeds. Updating Movie
		// here made a later artwork failure expose new text beside the old poster.
		return tx.Task.UpdateOneID(taskID).SetPayload(encoded).Exec(ctx)
	}); err != nil {
		return fmt.Errorf("save movie metadata checkpoint: %w", err)
	}
	return nil
}

// Finished marks the movie scrape as failed inside the completion transaction
// when a scrape job fails. Failures change the library view; successes
// only advance the download workflow projection.
func (service *Service) Finished(ctx context.Context, tx *ent.Tx, job tasks.Job, result error) (tasks.Change, error) {
	if result == nil {
		return tasks.ChangeOffline, nil
	}
	record, err := tx.Task.Get(ctx, job.ID)
	if err != nil {
		return 0, err
	}
	input, err := tasks.DecodePayload[struct {
		MetadataPayload
		Completed bool `json:"completed"`
	}](record.Payload)
	if err != nil {
		return 0, err
	}
	// Read the durable task, not a possibly stale worker payload, for the SQL
	// publication receipt. A failure to retire its journal is not a failed
	// movie publication: preserve the already-committed visible state.
	if input.Completed {
		return tasks.ChangeOffline, nil
	}
	update := tx.Movie.Update().Where(
		movie.IDEQ(input.MovieID),
		movie.ManualCodeEQ(input.ManualCode),
		movie.HasFilesWith(database.LibraryFiles(input.Source)),
	)
	if !input.Rebuild {
		update.Where(movie.ScrapeStatusNEQ(movie.ScrapeStatusDone))
	}
	if err := update.SetScrapeStatus(movie.ScrapeStatusFailed).Exec(ctx); err != nil {
		return 0, err
	}
	return tasks.ChangeLibrary, nil
}

// MovieDirectory groups a movie's indexed videos with the current directory listing.
type MovieDirectory struct {
	ID       string
	VideoIDs map[string]bool
	Files    []pan.File
}

// Directories returns all directories containing media files for the movie.
func (service *Service) Directories(ctx context.Context, sess drive.Session, input MetadataPayload) ([]MovieDirectory, error) {
	files, err := service.db.File.Query().Where(database.LibraryFiles(input.Source), file.MovieIDEQ(input.MovieID)).
		Order(ent.Asc(file.FieldParentID), ent.Asc(file.FieldFileID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load movie file directories: %w", err)
	}
	if len(files) == 0 {
		return nil, domain.E(domain.KindNotFound, "影片已没有媒体文件，请重新扫描", nil)
	}
	var result []MovieDirectory
	byID := make(map[string]int)
	for _, entry := range files {
		index, found := byID[entry.ParentID]
		if !found {
			index = len(result)
			byID[entry.ParentID] = index
			result = append(result, MovieDirectory{ID: entry.ParentID, VideoIDs: make(map[string]bool)})
		}
		result[index].VideoIDs[entry.FileID] = true
	}
	for i := range result {
		directory := &result[i]
		directory.Files, err = service.directoryEntries(ctx, sess, directory.ID)
		if err != nil {
			return nil, fmt.Errorf("read metadata directory: %w", err)
		}
		present := 0
		for _, entry := range directory.Files {
			if !entry.IsDirectory && domain.IsVideo(entry.Name) && entry.Size >= domain.MinVideoSize {
				if directory.VideoIDs[entry.ID] {
					present++
				}
			}
		}
		if present == 0 {
			return nil, domain.E(domain.KindNotFound, "视频文件已删除或移动，请重新扫描", nil)
		}
	}
	return result, nil
}

// directoryEntries retrieves directory entries using a short-lived cache to avoid redundant pagination of large folders.
func (service *Service) directoryEntries(ctx context.Context, sess drive.Session, dirID string) ([]pan.File, error) {
	key := dirID
	if src := sess.Source(); src.AccountID != "" {
		key = src.AccountID + ":" + dirID
	}

	service.dirMu.Lock()
	service.pruneDirCache(time.Now())
	entry, ok := service.dirCache[key]
	service.dirMu.Unlock()
	if ok {
		return slices.Clone(entry.files), nil
	}

	files, err := drive.DirectoryEntries(ctx, sess, dirID)
	if err != nil {
		return nil, err
	}
	// Large directories are still read in full, but must not displace the entire cache.
	if len(files) > maxDirCacheFiles {
		return files, nil
	}

	service.dirMu.Lock()
	now := time.Now()
	service.dirCache[key] = dirCacheEntry{
		files:     files,
		expiresAt: now.Add(defaultDirCacheTTL),
	}
	service.pruneDirCache(now)
	service.dirMu.Unlock()

	return slices.Clone(files), nil
}

// pruneDirCache reclaims expired listings and evicts the oldest listings when over budget.
// The caller must hold dirMu. Idle caches remain bounded without a background worker.
func (service *Service) pruneDirCache(now time.Time) {
	fileCount := 0
	for key, entry := range service.dirCache {
		if !now.Before(entry.expiresAt) {
			delete(service.dirCache, key)
			continue
		}
		fileCount += len(entry.files)
	}
	for len(service.dirCache) > maxDirCacheEntries || fileCount > maxDirCacheFiles {
		var oldestKey string
		var oldestExpiry time.Time
		for key, entry := range service.dirCache {
			if oldestExpiry.IsZero() || entry.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = key, entry.expiresAt
			}
		}
		fileCount -= len(service.dirCache[oldestKey].files)
		delete(service.dirCache, oldestKey)
	}
}

func (service *Service) resolveMetadata(ctx context.Context, input *Payload) error {
	ref := domain.MovieRef{Code: input.Code, JavDBID: input.JavDBID, Refresh: input.Rebuild}
	resolve := service.metadata.Resolve
	if input.SelectedJavDBID != "" {
		ref.JavDBID = input.SelectedJavDBID
		resolve = service.metadata.Fallback
	}
	result, err := resolve(ctx, ref)
	if err != nil {
		return err
	}
	if input.SelectedJavDBID != "" && (result.Detail.ID != input.SelectedJavDBID || !codeid.IsFormatEquivalent(result.Detail.Code, input.Code)) {
		return domain.E(domain.KindConflict, "所选 JavDB 条目与当前影片番号不符", nil)
	}
	input.Document = DetailNFO(result.Detail)
	input.Document.Images = result.Images
	input.Code = codeid.Normalize(input.Document.Code)
	input.Document.Code = input.Code
	return nil
}
