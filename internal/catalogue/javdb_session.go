package catalogue

import (
	"context"
	"fmt"
	"os"

	"github.com/ppxb/miyabi/internal/atomicfile"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javdb"
)

func (service *Service) JavDBSessionConfigured() bool {
	client, ok := service.javdb.(*javdb.Client)
	return ok && client.HasSession()
}

func (service *Service) LoginJavDB(ctx context.Context, username, password string) error {
	client, ok := service.javdb.(*javdb.Client)
	if !ok || service.javdbSessionPath == "" {
		return domain.E(domain.KindUpstream, "JavDB 登录不可用", nil)
	}
	service.javdbSessionMu.Lock()
	defer service.javdbSessionMu.Unlock()
	token, err := client.Login(ctx, username, password)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := atomicfile.WriteFile(service.javdbSessionPath, []byte(token), 0o600); err != nil {
		return fmt.Errorf("save JavDB session: %w", err)
	}
	client.SetSessionToken(token)
	return nil
}

func (service *Service) LogoutJavDB(ctx context.Context) error {
	client, ok := service.javdb.(*javdb.Client)
	if !ok || service.javdbSessionPath == "" {
		return nil
	}
	service.javdbSessionMu.Lock()
	defer service.javdbSessionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(service.javdbSessionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove JavDB session: %w", err)
	}
	client.SetSessionToken("")
	return nil
}
