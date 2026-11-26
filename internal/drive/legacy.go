package drive

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/panaccount"
	"github.com/ppxb/miyabi/internal/ent/storagesource"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/source"
)

const (
	legacyCredentialsKey = "pan.credentials"
	legacyDirectoryKey   = "pan.library_directory"
	accountMigrationKey  = "pan.migrated_to_accounts"
)

func migrateLegacyAccount(ctx context.Context, db *ent.Client, client Client) error {
	done, _, err := database.LoadSetting[bool](ctx, db, accountMigrationKey)
	if err != nil || done {
		return err
	}
	tokens, found, err := database.LoadSetting[pan.Tokens](ctx, db, legacyCredentialsKey)
	if err != nil || !found || tokens.AccessToken == "" {
		return err
	}
	mount, _, err := database.LoadSetting[mountRecord](ctx, db, legacyDirectoryKey)
	if err != nil {
		return err
	}

	if mount.AccountID == "" {
		// Login without a selected directory did not persist an account ID.
		hasAccount, err := db.PanAccount.Query().Exist(ctx)
		if err != nil || hasAccount {
			return err
		}
		account, err := client.Account(ctx, tokens.AccessToken)
		if err != nil {
			slog.WarnContext(ctx, "defer migration of 115 credentials without a bound directory", "error", err)
			return nil
		}
		if account.ID == "" {
			return nil
		}
		mount.AccountID = account.ID
	}

	return ent.WithTx(ctx, db, func(tx *ent.Tx) error {
		hasAccount, err := tx.PanAccount.Query().Where(panaccount.AccountID(mount.AccountID)).Exist(ctx)
		if err != nil {
			return err
		}
		if !hasAccount {
			err := tx.PanAccount.Create().SetAccountID(mount.AccountID).
				SetAccessToken(tokens.AccessToken).SetRefreshToken(tokens.RefreshToken).SetExpiresAt(tokens.ExpiresAt).
				SetDirectoryID(mount.ID).SetDirectoryName(mount.Name).SetDirectoryPath(mount.Path).
				Exec(ctx)
			if err != nil {
				return err
			}
			if mount.ID != "" {
				if err := migrateLegacySource(ctx, tx, mount); err != nil {
					return err
				}
			}
		}
		return database.SaveSetting(ctx, tx.Client(), accountMigrationKey, true)
	})
}

func migrateLegacySource(ctx context.Context, tx *ent.Tx, mount mountRecord) error {
	sources, err := tx.StorageSource.Query().Order(ent.Desc(storagesource.FieldPosition)).All(ctx)
	if err != nil {
		return err
	}
	kind := string(source.Kind115)
	for _, row := range sources {
		if row.Kind == kind && row.Config["account_id"] == mount.AccountID {
			return nil
		}
	}
	position := 0
	if len(sources) > 0 {
		position = sources[0].Position + 1
	}
	name := strings.TrimSpace(mount.Name)
	if name == "" {
		name = "115"
	}
	if err := tx.StorageSource.Create().SetID(uuid.NewString()).SetKind(kind).SetName(name).
		SetConfig(map[string]string{"account_id": mount.AccountID}).SetPosition(position).Exec(ctx); err != nil {
		return fmt.Errorf("migrate the bound 115 source: %w", err)
	}
	return nil
}
