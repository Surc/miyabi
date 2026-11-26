package monitor

import (
	"context"
	"sync"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/subscription"
	"github.com/ppxb/miyabi/internal/magnet"
	"github.com/ppxb/miyabi/internal/tasks"
)

type mockDiscoverer struct {
	mu          sync.Mutex
	actorMovies map[string][]domain.Movie
	browseErr   error
}

func newMockDiscoverer() *mockDiscoverer {
	return &mockDiscoverer{actorMovies: map[string][]domain.Movie{}}
}

func (m *mockDiscoverer) BrowseMovies(_ context.Context, options domain.BrowseOptions) ([]domain.Movie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.browseErr != nil {
		return nil, m.browseErr
	}
	return m.actorMovies[options.EntityID], nil
}

// mockFANZA knows the actresses in ids by name and lists works by actress id.
type mockFANZA struct {
	mu         sync.Mutex
	ids        map[string]string
	works      map[string][]domain.Movie
	resolveErr error
}

func (m *mockFANZA) ResolveActress(_ context.Context, names []string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resolveErr != nil {
		return "", m.resolveErr
	}
	for _, name := range names {
		if id := m.ids[name]; id != "" {
			return id, nil
		}
	}
	return "", nil
}

func (m *mockFANZA) ActressWorks(_ context.Context, id string) ([]domain.Movie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.works[id], nil
}

type fixture struct {
	service  *Service
	discover *mockDiscoverer
	fanza    *mockFANZA
	// identities overrides the default library actor, named by its key and
	// browsed on JavDB under the same id.
	identities map[string]ActorIdentity
	tasks      *tasks.Service
	client     *ent.Client
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f := fixture{discover: newMockDiscoverer(), identities: map[string]ActorIdentity{},
		fanza: &mockFANZA{ids: map[string]string{}, works: map[string][]domain.Movie{}},
		tasks: tasks.NewService(store.Client, tasks.NewRegistry()), client: store.Client}
	lookup := func(_ context.Context, key string) (ActorIdentity, error) {
		if identity, ok := f.identities[key]; ok {
			if identity.Key == "" {
				identity.Key = key
			}
			return identity, nil
		}
		return ActorIdentity{Key: key, Name: key, Names: []string{key}, JavdbID: key}, nil
	}
	f.service = New(store.Client, f.discover, f.fanza, lookup, f.tasks)
	return f
}

func TestActorFollowLifecycle(t *testing.T) {
	f, ctx := newFixture(t), t.Context()
	item, err := f.service.AddActor(ctx, "a1")
	if err != nil || item.TargetID != "a1" {
		t.Fatalf("AddActor: %#v %v", item, err)
	}
	paused, err := f.service.Update(ctx, item.ID, UpdateOptions{Status: new("paused")})
	if err != nil || paused.Status != subscription.StatusPaused {
		t.Fatalf("pause: %#v %v", paused, err)
	}
	resumed, err := f.service.AddActor(ctx, "a1")
	if err != nil || resumed.ID != item.ID || resumed.Status != subscription.StatusActive {
		t.Fatalf("re-following a paused actor must resume it: %#v %v", resumed, err)
	}
	if err := f.service.Remove(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Remove(ctx, item.ID); !ent.IsNotFound(err) {
		t.Fatalf("removing a missing subscription must report not found, got %v", err)
	}
}

func TestRemovingActorDropsOnlyItsOwnWorks(t *testing.T) {
	f, ctx := newFixture(t), t.Context()
	shared := domain.Movie{ID: "shared", Code: "SHARED-001", ReleaseDate: "2099-01-01"}
	for _, id := range []string{"act1", "act2"} {
		f.discover.actorMovies[id] = []domain.Movie{{ID: "new-" + id, Code: "NEW-" + id, ReleaseDate: "2099-01-01"}, shared}
	}
	// The co-starred work is recorded once, under the first actor.
	first, _ := f.service.AddActor(ctx, "act1")
	second, _ := f.service.AddActor(ctx, "act2")
	if err := f.service.Remove(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	feed, err := f.service.ActorFeed(ctx, 0, 1, 10)
	if err != nil || len(feed) != 2 {
		t.Fatalf("only the unfollowed actor's own work must leave the feed: %#v %v", feed, err)
	}
	for _, item := range feed {
		if item.TargetID == "new-act1" || *item.OriginID != second.ID {
			t.Fatalf("remaining works must belong to the followed actor: %#v", feed)
		}
	}
}

func TestSubscriptionSettings(t *testing.T) {
	f, ctx := newFixture(t), t.Context()
	cfg, err := f.service.Config(ctx)
	if err != nil || cfg.CheckTime != "00:00" || cfg.Preferences != magnet.DefaultPreferences() {
		t.Fatalf("default config mismatch: %#v %v", cfg, err)
	}
	cfg.CheckTime, cfg.Preferences.Subtitle = "05:30", magnet.PreferenceRequired
	cfg.Download.AutoSwitch = false
	if _, err := f.service.UpdateConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if saved, err := f.service.Config(ctx); err != nil || saved != cfg {
		t.Fatalf("round trip mismatch: %#v %v", saved, err)
	}
	normalized, err := f.service.UpdateConfig(ctx, Config{CheckTime: ""})
	if err != nil || normalized.CheckTime != "00:00" {
		t.Fatalf("expected normalized check_time 00:00, got %#v %v", normalized, err)
	}
	for name, mutate := range map[string]func(*Config){
		"hour out of range": func(c *Config) { c.CheckTime = "25:00" },
		"missing minutes":   func(c *Config) { c.CheckTime = "4" },
		"unknown level":     func(c *Config) { c.Preferences.HD = "sometimes" },
	} {
		bad := cfg
		mutate(&bad)
		if _, err := f.service.UpdateConfig(ctx, bad); !domain.IsKind(err, domain.KindInvalid) {
			t.Errorf("%s: expected KindInvalid, got %v", name, err)
		}
	}
}

func TestSubscriptionTargetsAndPagination(t *testing.T) {
	f, ctx := newFixture(t), t.Context()
	f.discover.actorMovies["act1"] = []domain.Movie{
		{ID: "m-spawned", Code: "SPAWN-001", Title: "Spawned Movie", ReleaseDate: "2099-11-01"},
	}
	first, err := f.service.AddActor(ctx, "act1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.AddActor(ctx, "act2")
	if err != nil {
		t.Fatal(err)
	}
	targets, err := f.service.Targets(ctx, "")
	if err != nil || len(targets) != 2 {
		t.Fatalf("expected only the two followed actors: %#v %v", targets, err)
	}
	movieTargets, err := f.service.Targets(ctx, "movie")
	if err != nil || len(movieTargets) != 0 {
		t.Fatalf("the actor work must not appear as a follow target: %#v %v", movieTargets, err)
	}
	page1, err := f.service.List(ctx, "actor", 1, 1)
	if err != nil || len(page1) != 1 || page1[0].ID != second.ID {
		t.Fatalf("first actor page: %#v %v", page1, err)
	}
	page2, err := f.service.List(ctx, "actor", 2, 1)
	if err != nil || len(page2) != 1 || page2[0].ID != first.ID {
		t.Fatalf("second actor page: %#v %v", page2, err)
	}
	for _, id := range []int{0, first.ID} {
		feed, err := f.service.ActorFeed(ctx, id, 1, 10)
		if err != nil || len(feed) != 1 || feed[0].TargetID != "m-spawned" {
			t.Fatalf("expected one work in feed for %d: %#v %v", id, feed, err)
		}
	}
}

func TestActorSubscription_ConstraintErrorRecoversWithoutRecursion(t *testing.T) {
	f, ctx := newFixture(t), t.Context()
	var wg sync.WaitGroup
	n := 10
	results := make([]Item, n)
	errors := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errors[idx] = f.service.AddActor(ctx, "act-race")
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errors[i] != nil {
			t.Fatalf("expected concurrent AddActor to succeed via constraint recovery, got error at %d: %v", i, errors[i])
		}
		if results[i].ID != results[0].ID {
			t.Fatalf("expected all results to have the same ID, got %d vs %d", results[i].ID, results[0].ID)
		}
	}

	count, err := f.client.Subscription.Query().Where(subscription.KindEQ(subscription.KindActor), subscription.TargetIDEQ(actorTargetPrefix+"act-race")).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 subscription created, got %d", count)
	}
}
