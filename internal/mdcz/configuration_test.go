package mdcz

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestConfigurationReadsBypassJobQueueAndNeverExposeSecrets(t *testing.T) {
	s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/configuration" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected configuration request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(Configuration{
			Health:  Health{Protocol: Protocol, Revision: EngineRevision, ConfigurationVersion: ConfigurationVersion, ConfigurationID: "profile-1"},
			Values:  map[string]json.RawMessage{"sites": json.RawMessage(`["dmm"]`), "proxy_url": json.RawMessage(`"http://private:secret@proxy.test"`), "llm_key": json.RawMessage(`"private-key"`)},
			Secrets: map[string]bool{"proxy_url": true, "llm_key": true}, AvailableSites: []string{"dmm"},
		})
	})
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	value, err := s.Configuration(ctx)
	if err != nil || value.ConfigurationID != "profile-1" || !value.Secrets["llm_key"] {
		t.Fatalf("configuration unavailable during a job: %+v %v", value, err)
	}
	body, _ := json.Marshal(value)
	if strings.Contains(string(body), "private") || strings.Contains(string(body), "proxy.test") {
		t.Fatalf("configuration exposed credentials: %s", body)
	}
}

func TestConfigurationUpdatesPreserveOmittedSecretsAndMapSafeErrors(t *testing.T) {
	for _, scenario := range []struct {
		status int
		kind   domain.Kind
	}{
		{400, domain.KindInvalid}, {409, domain.KindConflict}, {503, domain.KindConflict}, {404, domain.KindUpstream},
	} {
		calls := 0
		s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			var input ConfigurationUpdate
			if r.Method != http.MethodPut || json.NewDecoder(r.Body).Decode(&input) != nil {
				t.Error("expected configuration update")
			}
			if _, exists := input.Values["llm_key"]; exists || string(input.Values["javdb_cookie"]) != `""` {
				t.Error("secret omission and explicit clear lost")
			}
			w.WriteHeader(scenario.status)
			w.Write([]byte("private upstream credentials"))
		})
		if _, err := s.UpdateConfiguration(t.Context(), ConfigurationUpdate{}); !domain.IsKind(err, domain.KindInvalid) || calls != 0 {
			t.Fatalf("invalid version reached the bridge: %v", err)
		}
		_, err := s.UpdateConfiguration(t.Context(), ConfigurationUpdate{Version: ConfigurationVersion, ID: "profile-1", Values: map[string]json.RawMessage{"javdb_cookie": json.RawMessage(`""`)}})
		if !domain.IsKind(err, scenario.kind) || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe or incorrect bridge error: %v", err)
		}
	}
}

func TestSynopsisRefreshesAfterConfigurationChangesWithoutErasingCacheOnFailure(t *testing.T) {
	id, plot, requests, failed := "profile-1", "First language", 0, false
	s := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			json.NewEncoder(w).Encode(Health{Protocol: Protocol, Revision: EngineRevision, ConfigurationVersion: ConfigurationVersion, ConfigurationID: id})
			return
		}
		requests++
		if failed {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"protocol": Protocol, "revision": EngineRevision, "configuration_id": id, "detail": map[string]string{"code": "TEST-001", "title": "Fixture", "plot": plot}})
	})
	for range 2 {
		value, err := s.Synopsis(t.Context(), "TEST-001")
		if err != nil || value.Plot != plot {
			t.Fatalf("initial synopsis: %+v %v", value, err)
		}
	}
	if requests != 1 {
		t.Fatal("valid cache repeated the metadata request")
	}
	id, plot = "profile-2", "Changed language"
	value, err := s.Synopsis(t.Context(), "TEST-001")
	if err != nil || value.Plot != plot || requests != 2 {
		t.Fatalf("old configuration cache reused: %+v %v", value, err)
	}
	file := s.synopsisPath(s.config(), "TEST-001")
	before, _ := os.ReadFile(file)
	id, failed = "profile-3", true
	value, err = s.Synopsis(t.Context(), "TEST-001")
	after, _ := os.ReadFile(file)
	if err == nil || value.Plot != "" || string(before) != string(after) {
		t.Fatal("failed refresh returned stale success or erased the cache")
	}
	s.settings.Enabled = false
	value, err = s.Synopsis(t.Context(), "TEST-001")
	if err != nil || value.Enabled || value.Plot != plot {
		t.Fatalf("disabled offline cache unavailable: %+v %v", value, err)
	}
}
