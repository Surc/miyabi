package export

import (
	"errors"
	"testing"
)

func TestBeforeWriteRecoveryBlocksAllWriters(t *testing.T) {
	manager := NewManager(Config{EmbyDir: t.TempDir()})
	manager.EnablePublicationRecovery(t.TempDir(), func(*Publication) (bool, error) { return false, nil })
	blocked := errors.New("pending subtitle registration")
	manager.SetBeforeWriteRecovery(func(Config) error { return blocked })
	called := false
	if err := manager.WithConfig(func(Config) error { called = true; return nil }); !errors.Is(err, blocked) || called {
		t.Fatalf("pending recovery allowed writer: %v called=%v", err, called)
	}
	if !manager.PublicationStatus().Blocked {
		t.Fatal("blocked snapshot missing")
	}
	manager.SetBeforeWriteRecovery(func(Config) error { return nil })
	if err := manager.WithConfig(func(Config) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("recovered writer blocked: %v", err)
	}
	if manager.PublicationStatus().Blocked {
		t.Fatal("recovered snapshot stale")
	}
}
