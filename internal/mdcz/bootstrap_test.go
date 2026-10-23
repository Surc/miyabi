package mdcz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapOnlySuppliesConnectionAndKeepsDisabledDefaults(t *testing.T) {
	current := Settings{Position: "bottomLeft", Badges: true}
	if value, err := bootstrapConnection(current, ""); err != nil || value != current {
		t.Fatal("optional bootstrap changed standalone defaults")
	}
	file := filepath.Join(t.TempDir(), "connection.json")
	key := strings.Repeat("k", 32)
	if err := os.WriteFile(file, []byte(`{"server_url":"http://127.0.0.1:3840","api_key":"`+key+`","enabled":true,"translate":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := bootstrapConnection(current, file)
	if err != nil || value.Enabled || value.Translate || !value.Badges || value.APIKey != key {
		t.Fatalf("bootstrap changed user-facing defaults: %v", err)
	}
	if err := os.WriteFile(file, []byte(`{"server_url":"http://user:private@host","api_key":"private"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err = bootstrapConnection(current, file)
	if err == nil || strings.Contains(err.Error(), "private") || value != current {
		t.Fatal("invalid provisioning was accepted or exposed")
	}
}
