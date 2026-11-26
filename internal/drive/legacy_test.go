package drive

import (
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestMigrateLegacyMountLocallyAndOnlyOnce(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.Client
	tokens := pan.Tokens{AccessToken: "legacy-token", RefreshToken: "legacy-refresh", ExpiresAt: time.Now().Add(time.Hour)}
	mount := mountRecord{AccountID: "account-a", LibraryDirectory: domain.LibraryDirectory{
		ID: "directory-a", Name: "Movies", Path: "/Movies",
	}}
	if err := database.SaveSetting(ctx, db, legacyCredentialsKey, tokens); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveSetting(ctx, db, legacyDirectoryKey, mount); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyAccount(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	account := db.PanAccount.Query().OnlyX(ctx)
	if account.AccountID != mount.AccountID || account.DirectoryID != mount.ID ||
		account.AccessToken != tokens.AccessToken || account.RefreshToken != tokens.RefreshToken {
		t.Fatal("legacy account or directory was not carried over")
	}
	source := db.StorageSource.Query().OnlyX(ctx)
	if source.Kind != "115" || !source.Enabled || source.Config["account_id"] != mount.AccountID {
		t.Fatalf("migrated storage source = %#v", source)
	}
	db.PanAccount.DeleteOne(account).ExecX(ctx)
	if err := migrateLegacyAccount(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	if db.PanAccount.Query().ExistX(ctx) {
		t.Fatal("migration restored an account explicitly removed after upgrade")
	}
}
