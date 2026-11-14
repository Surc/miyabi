package mdcz

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
)

// effective is the sole authority for all bridge requests. The legacy database
// values are retained for rollback, never consulted when managed mode is set.
func (s *Service) effective(cfg Settings) (Settings, error) {
	cfg.WebPort = 0
	if s.managedPath == "" {
		cfg.Mode = "external"
		cfg.Generation = digest([]byte(cfg.ServerURL))
		return cfg, s.initializationError
	}
	cfg.Managed, cfg.Mode = true, "managed"
	cfg.ServerURL, cfg.APIKey = "", ""
	f, err := os.Open(s.managedPath)
	if err != nil {
		return cfg, connectionUnavailable()
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(body) > 8192 {
		return cfg, connectionUnavailable()
	}
	var connection struct {
		ServerURL  string `json:"server_url"`
		APIKey     string `json:"api_key"`
		Generation string `json:"generation"`
		WebPort    int    `json:"web_port"`
	}
	if json.Unmarshal(body, &connection) != nil || !hashPattern.MatchString(connection.Generation) || connection.Generation != digest([]byte(connection.ServerURL)) {
		return cfg, connectionUnavailable()
	}
	cfg.ServerURL, cfg.APIKey, cfg.Generation = connection.ServerURL, connection.APIKey, connection.Generation
	check := cfg
	check.Enabled = true
	if validate(check) != nil {
		cfg.ServerURL, cfg.APIKey = "", ""
		return cfg, connectionUnavailable()
	}
	if connection.WebPort >= 1024 && connection.WebPort <= 65535 {
		cfg.WebPort = connection.WebPort
	}
	return cfg, s.initializationError
}

func connectionUnavailable() error {
	return domain.E(domain.KindUpstream, "包内 MDCz 连接不可用，请检查安装配置；不会回退到旧外部服务", nil)
}

// Fallback preserves the existing exact native identity lookup explicitly; it
// does not imply falling back from a failed enhanced scrape to a native scrape.
func (s *Service) Fallback(ctx context.Context, ref domain.MovieRef) (domain.MovieMetadata, error) {
	return s.base.Fallback(ctx, ref)
}

type attemptContextKey struct{}
type attemptContext struct {
	settings Settings
	health   Health
}

func (s *Service) attemptSettings(ctx context.Context) (Settings, error) {
	if attempt, ok := ctx.Value(attemptContextKey{}).(attemptContext); ok {
		current, err := s.effective(s.config())
		if attempt.settings.Enabled && err != nil {
			return attempt.settings, err
		}
		if current.Generation != attempt.settings.Generation {
			return attempt.settings, domain.E(domain.KindConflict, "MDCz 连接目标已变化，请重新刮削", nil)
		}
		// Credential rotation at the same target does not alter business options.
		attempt.settings.APIKey = current.APIKey
		return attempt.settings, nil
	}
	return s.effective(s.config())
}

// BeginAttempt freezes business options and bridge identity for one scrape.
// Only the non-secret digest is checkpointed; credentials stay in request memory.
func (s *Service) BeginAttempt(ctx context.Context, previous string) (context.Context, string, error) {
	cfg, err := s.effective(s.config())
	var health Health
	if cfg.Enabled {
		if err != nil {
			return ctx, "", err
		}
		healthContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.json(healthContext, cfg, "GET", "/v1/health", nil, &health)
		cancel()
		if err != nil {
			return ctx, "", err
		}
		if !compatibleConfiguration(health) {
			return ctx, "", domain.E(domain.KindUpstream, "MDCz 桥接版本不匹配", nil)
		}
	}
	data, _ := json.Marshal([]any{cfg.Enabled, cfg.Generation, cfg.Translate, cfg.Badges, cfg.Position, health.Revision, health.ConfigurationID})
	stamp := digest(data)
	if previous != "" && previous != stamp {
		return ctx, "", domain.E(domain.KindConflict, "刮削设置已变化，请重新刮削以准备一致的资料和图片", nil)
	}
	return context.WithValue(ctx, attemptContextKey{}, attemptContext{settings: cfg, health: health}), stamp, nil
}
