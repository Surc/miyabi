package drive

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/syncx"
	"golang.org/x/sync/singleflight"
)

// Client abstracts the upstream 115 API calls.
type Client interface {
	Close()
	Account(context.Context, string) (pan.Account, error)
	BeginLogin(context.Context) (*pan.Login, error)
	LoginStatus(context.Context, *pan.Login) (pan.LoginState, error)
	ExchangeToken(context.Context, *pan.Login) (pan.Tokens, error)
	RefreshToken(context.Context, string) (pan.Tokens, error)
	List(context.Context, string, string, int, int) (pan.FilePage, error)
	Info(context.Context, string, string) (pan.FileInfo, error)
	RecycleFile(context.Context, string, string, string) error
	ReadMetadata(context.Context, string, string, int64) ([]byte, error)
	AddOffline(context.Context, string, string, string) (string, error)
	RemoveOffline(context.Context, string, string) error
	OfflineTasks(context.Context, string, int) (pan.OfflinePage, error)
	PlayURL(context.Context, string, string, string) ([]pan.PlaySource, error)
	DownloadURL(context.Context, string, string, string) (string, error)
	OpenMedia(context.Context, string, string, http.Header) (*http.Response, error)
}

type mountRecord struct {
	AccountID string `json:"account_id"`
	domain.LibraryDirectory
}

func (r mountRecord) source() domain.LibrarySource {
	return domain.LibrarySource{AccountID: r.AccountID, Directory: r.LibraryDirectory}
}

// Drive coordinates the registry of 115 accounts and their mounts. Every account
// is independent; there is no primary account.
type Drive struct {
	database *ent.Client
	client   Client

	mu     sync.Mutex
	commit syncx.ContextLock
	work   sync.WaitGroup
	closed bool

	login         qrLogin
	mountListener MountListener

	// Credential/authorization versions are runtime invalidation counters for
	// in-flight sessions; account data itself lives in PanAccount rows.
	accountCredVersions map[string]uint64
	accountAuthVersions map[string]uint64
	accountRefresh      singleflight.Group
	refreshStop         chan struct{}
}

func New(ctx context.Context, db *ent.Client) (*Drive, error) {
	return NewWithClient(ctx, db, pan.New())
}

func NewWithClient(ctx context.Context, db *ent.Client, client Client) (*Drive, error) {
	if err := migrateLegacyAccount(ctx, db, client); err != nil {
		client.Close()
		return nil, fmt.Errorf("migrate legacy 115 account: %w", err)
	}
	d := &Drive{
		database:            db,
		client:              client,
		refreshStop:         make(chan struct{}),
		accountCredVersions: make(map[string]uint64),
		accountAuthVersions: make(map[string]uint64),
	}
	d.login.drive = d
	return d, nil
}

func (d *Drive) Close() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		if d.refreshStop != nil {
			close(d.refreshStop)
			d.refreshStop = nil
		}
	}
	d.mu.Unlock()
	d.work.Wait()
	d.client.Close()
}

func (d *Drive) closedState() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// StartWork registers work that Close must wait for, such as a remote
// mutation that has to be recorded once started. It returns false once the
// drive is closed.
func (d *Drive) StartWork() (func(), bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, false
	}
	d.work.Add(1)
	return d.work.Done, true
}

// Commit executes a database transaction under the drive commit lock.
func (d *Drive) Commit(ctx context.Context, fn func(tx *ent.Tx) error) error {
	if err := d.commit.Lock(ctx); err != nil {
		return err
	}
	defer d.commit.Unlock()
	return ent.WithTx(ctx, d.database, fn)
}

// WithMounts keeps the mounted sources stable during a local operation.
func (d *Drive) WithMounts(ctx context.Context, fn func([]domain.LibrarySource) error) error {
	if err := d.commit.Lock(ctx); err != nil {
		return err
	}
	defer d.commit.Unlock()
	mounts, err := d.Mounts(ctx)
	if err != nil {
		return err
	}
	return fn(mounts)
}

// OpenMedia streams media from 115 CDN directly.
func (d *Drive) OpenMedia(ctx context.Context, method, address string, headers http.Header) (*http.Response, error) {
	return d.client.OpenMedia(ctx, method, address, headers)
}

// Source returns one mounted account's library source (the first by account id).
// It is a display/test convenience; no account is privileged.
func (d *Drive) Source() *domain.LibrarySource {
	mounts, err := d.Mounts(context.Background())
	if err != nil || len(mounts) == 0 {
		return nil
	}
	return &mounts[0]
}
