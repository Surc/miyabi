package javinizer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnsetOrDisabledConfigPreservesDefaultProvider(t *testing.T) {
	config, err := LoadConfig("")
	if err != nil || config.Enabled {
		t.Fatal(config, err)
	}
	client, err := New(config)
	if err != nil || client != nil {
		t.Fatalf("unset connection enabled an engine: %v %v", client, err)
	}
	path := filepath.Join(t.TempDir(), "connection.json")
	if err := os.WriteFile(path, []byte(`{"enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	config, err = LoadConfig(path)
	if err != nil || config.Enabled {
		t.Fatal(config, err)
	}
}

func TestLoadConnectionConfig(t *testing.T) {
	want := Config{Enabled: true, ServerURL: "http://127.0.0.1:8765/", APIToken: testToken, TimeoutSeconds: 60}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "connection.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil || got != want {
		t.Fatalf("config = %+v, error = %v", got, err)
	}
	client, err := New(got)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.endpoint != "http://127.0.0.1:8765/api/v1/scrape" || client.timeout != time.Minute {
		t.Fatal("connection settings not applied")
	}
}

func TestLoadConfigRejectsExplicitInvalidConfiguration(t *testing.T) {
	for _, body := range []string{
		"", "[", `[]`, `null`, `{} {}`, `{"enabled":true,"api_tokne":"secret"}`,
		`{"enabled":true}`, `{"enabled":"true"}`, strings.Repeat(" ", 8193),
	} {
		path := filepath.Join(t.TempDir(), "connection.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("accepted invalid configuration of %d bytes", len(body))
		}
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing-private-file"), t.TempDir()} {
		_, err := LoadConfig(path)
		if err == nil || strings.Contains(err.Error(), path) {
			t.Fatalf("read failure missing or exposed path: %v", err)
		}
	}
}

func TestConfigRejectsCredentialsAndNonOriginURLs(t *testing.T) {
	for _, serverURL := range []string{
		"", "localhost:8765", "ftp://example.com", "http://", "http://user:secret@example.com",
		"https://example.com/api", "https://example.com/?token=secret", "https://example.com/?",
		"https://example.com/#secret", "https://example.com/%2f", "http://example.com:wrong",
	} {
		config := Config{Enabled: true, ServerURL: serverURL, APIToken: testToken}
		if _, err := New(config); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("unsafe URL accepted or echoed: %q, %v", serverURL, err)
		}
	}
	for _, serverURL := range []string{"http://127.0.0.1:8765", "http://javinizer:8765/", "https://engine.example", "http://[::1]:8765"} {
		client, err := New(Config{Enabled: true, ServerURL: serverURL, APIToken: testToken})
		if err != nil {
			t.Errorf("valid origin %q: %v", serverURL, err)
			continue
		}
		client.Close()
	}
}

func TestConfigBoundsCredentialsAndTimeout(t *testing.T) {
	config := Config{Enabled: true, ServerURL: "http://127.0.0.1:8765", APIToken: testToken}
	for _, token := range []string{"", "abc", " leading", "trailing ", "line\nbreak", "line\rbreak", "tab\tbreak", strings.Repeat("a", 513)} {
		next := config
		next.APIToken = token
		if _, err := New(next); err == nil {
			t.Error("invalid API token accepted")
		}
	}
	for _, seconds := range []int{-1, 1, 4, 121} {
		next := config
		next.TimeoutSeconds = seconds
		if _, err := New(next); err == nil {
			t.Errorf("timeout %d accepted", seconds)
		}
	}
	for _, seconds := range []int{0, 5, 120} {
		next := config
		next.TimeoutSeconds = seconds
		client, err := New(next)
		if err != nil {
			t.Errorf("valid timeout %d: %v", seconds, err)
			continue
		}
		if seconds == 0 && client.timeout != 45*time.Second {
			t.Error("default timeout must be bounded")
		}
		client.Close()
	}
}
