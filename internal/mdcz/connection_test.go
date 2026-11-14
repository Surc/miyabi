package mdcz

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/metadata"
)

func TestManagedConnectionDoesNotFallBackToLegacyDatabase(t *testing.T) {
	key := strings.Repeat("a", 32)
	path := filepath.Join(t.TempDir(), "connection.json")
	s := &Service{managedPath: path, settings: Settings{Enabled: true, ServerURL: "http://old-docker:3840", APIKey: "old-private", Position: "bottomLeft"}}
	cfg, err := s.effective(s.config())
	if err == nil || cfg.ServerURL != "" || cfg.APIKey != "" || !cfg.Managed {
		t.Fatal("missing managed file fell back")
	}
	connection := map[string]string{"server_url": "http://127.0.0.1:3840", "api_key": key, "generation": digest([]byte("http://127.0.0.1:3840"))}
	body, _ := json.Marshal(connection)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.effective(s.config())
	if err != nil || cfg.APIKey != key || cfg.ServerURL != connection["server_url"] {
		t.Fatalf("managed connection not authoritative: %v", err)
	}
	public, _ := json.Marshal(s.ExtensionSettings())
	if strings.Contains(string(public), key) || strings.Contains(string(public), "old-private") {
		t.Fatal("connection leaked secrets")
	}
	if s.config().ServerURL != "http://old-docker:3840" {
		t.Fatal("legacy rollback settings overwritten")
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := s.effective(s.config()); err == nil || cfg.APIKey != "" {
		t.Fatal("malformed authority accepted")
	}
}

func TestManagedOfflineModeCanBeDisabledWithoutBridge(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Service{db: store.Client, managedPath: filepath.Join(t.TempDir(), "missing"), settings: Settings{Enabled: true, ServerURL: "http://old-docker:3840", APIKey: "preserved-private", Position: "bottomLeft"}}
	change := Update{Settings: Settings{ServerURL: "http://127.0.0.1:3840", Position: "bottomLeft"}}
	if err := s.UpdateExtensionSettings(t.Context(), change); err != nil {
		t.Fatalf("offline switch blocked: %v", err)
	}
	if s.config().Enabled || s.config().APIKey != "preserved-private" {
		t.Fatal("disable lost rollback settings")
	}
	change.Enabled = true
	if err := s.UpdateExtensionSettings(t.Context(), change); err == nil {
		t.Fatal("enabled missing managed authority")
	}
	key := "replace"
	change.Enabled, change.APIKey = false, &key
	if err := s.UpdateExtensionSettings(t.Context(), change); !domain.IsKind(err, domain.KindConflict) {
		t.Fatalf("managed credentials editable: %v", err)
	}
}

func TestAttemptFreezesOptionsAndRejectsChangedConfigurationOnResume(t *testing.T) {
	id := "profile-one"
	calls := 0
	s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		health := Health{BridgeVersion: BridgeVersion, Capabilities: RequiredCapabilities, Protocol: Protocol, Revision: EngineRevision, ConfigurationVersion: ConfigurationVersion, ConfigurationID: id}
		if r.URL.Path == "/v1/health" {
			json.NewEncoder(w).Encode(health)
			return
		}
		calls++
		if r.Header.Get("X-Miyabi-MDCz-Configuration-ID") != "profile-one" || r.Header.Get("X-Miyabi-MDCz-Engine-Revision") != EngineRevision {
			t.Error("attempt guard missing")
		}
		var input map[string]any
		json.NewDecoder(r.Body).Decode(&input)
		if input["translate"] != false {
			t.Error("live settings replaced frozen options")
		}
		json.NewEncoder(w).Encode(bridgeMetadata{Health: health, MovieMetadata: domain.MovieMetadata{Detail: domain.MovieDetail{Movie: domain.Movie{Code: "TEST-001", Title: "Fixture"}}}})
	})
	ctx, stamp, err := s.BeginAttempt(t.Context(), "")
	if err != nil || stamp == "" || strings.Contains(stamp, "secret") {
		t.Fatalf("attempt not created: %v", err)
	}
	s.settings.Translate = true
	if _, err := s.Resolve(ctx, domain.MovieRef{Code: "TEST-001"}); err != nil {
		t.Fatal(err)
	}
	originalURL := s.settings.ServerURL
	s.settings.ServerURL = "http://changed.invalid"
	if _, err := s.Resolve(ctx, domain.MovieRef{Code: "TEST-001"}); !domain.IsKind(err, domain.KindConflict) {
		t.Fatal("changed connection used a stale target")
	}
	s.settings.ServerURL = originalURL
	if calls != 1 {
		t.Fatal("metadata missing")
	}
	if _, _, err := s.BeginAttempt(t.Context(), stamp); !domain.IsKind(err, domain.KindConflict) {
		t.Fatal("changed options resumed old candidate")
	}
	s.settings.Translate = false
	id = "profile-two"
	if _, _, err := s.BeginAttempt(t.Context(), stamp); !domain.IsKind(err, domain.KindConflict) {
		t.Fatal("changed profile resumed old candidate")
	}
	if _, err := s.Resolve(ctx, domain.MovieRef{Code: "TEST-001"}); !domain.IsKind(err, domain.KindConflict) {
		t.Fatal("mismatched response was accepted")
	}
}

func TestCompatibilityRejectsMissingCapabilitiesAndUnknownBridge(t *testing.T) {
	health := Health{BridgeVersion: BridgeVersion, Capabilities: RequiredCapabilities, Protocol: Protocol, Revision: EngineRevision, ConfigurationVersion: ConfigurationVersion, ConfigurationID: "profile"}
	if !compatibleConfiguration(health) {
		t.Fatal("pinned bridge rejected")
	}
	health.BridgeVersion = "future"
	if compatibleConfiguration(health) {
		t.Fatal("unknown bridge accepted")
	}
	health.BridgeVersion, health.Capabilities = BridgeVersion, []string{"metadata"}
	if compatibleConfiguration(health) {
		t.Fatal("missing capability accepted")
	}
}

func TestOptionalExtensionStorageDoesNotHideCoreDatabaseFailures(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	native, err := metadata.New(t.Context(), store.Client)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := New(t.Context(), store.Client, blocked, native)
	if err != nil || service == nil {
		t.Fatalf("optional storage stopped core initialization: %v", err)
	}
	if service.ExtensionSettings().Status != "unavailable" {
		t.Fatal("optional storage fault hidden")
	}
	if _, _, err := service.BeginAttempt(t.Context(), ""); err != nil {
		t.Fatalf("native mode blocked by optional storage: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := New(t.Context(), store.Client, t.TempDir(), native); err == nil {
		t.Fatal("core database failure was ignored")
	}
}
