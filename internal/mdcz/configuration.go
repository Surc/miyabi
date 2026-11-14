package mdcz

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/ppxb/miyabi/internal/domain"
)

// The bridge owns validation/defaults and durable configuration. Miyabi never
// stores a second copy or exposes secret values returned by a faulty bridge.
type Configuration struct {
	Health
	ConnectionGeneration string                     `json:"connection_generation"`
	Values               map[string]json.RawMessage `json:"values"`
	Secrets              map[string]bool            `json:"secrets"`
	AvailableSites       []string                   `json:"available_sites"`
}

type ConfigurationUpdate struct {
	ConnectionGeneration string                     `json:"connection_generation,omitempty"`
	Version              int                        `json:"configuration_version"`
	ID                   string                     `json:"configuration_id"`
	Values               map[string]json.RawMessage `json:"values"`
}

func (s *Service) Configuration(ctx context.Context) (Configuration, error) {
	return s.configuration(ctx, http.MethodGet, nil)
}

func (s *Service) UpdateConfiguration(ctx context.Context, input ConfigurationUpdate) (Configuration, error) {
	if input.Version != ConfigurationVersion || input.ID == "" || len(input.ID) > 128 || input.Values == nil {
		return Configuration{}, domain.E(domain.KindInvalid, "请重新读取 MDCz 配置后保存", nil)
	}
	return s.configuration(ctx, http.MethodPut, input)
}

func (s *Service) configuration(ctx context.Context, method string, input any) (Configuration, error) {
	var value Configuration
	cfg, err := s.effective(s.config())
	if err != nil {
		return Configuration{}, err
	}
	if change, ok := input.(ConfigurationUpdate); ok {
		if change.ConnectionGeneration == "" || change.ConnectionGeneration != cfg.Generation {
			return Configuration{}, domain.E(domain.KindConflict, "MDCz 连接已变化，请重新读取来源配置后再保存", nil)
		}
		// Target identity is owned by Miyabi; do not forward this field to MDCz.
		change.ConnectionGeneration = ""
		input = change
	}
	if err := s.json(ctx, cfg, method, "/v1/configuration", input, &value); err != nil {
		return Configuration{}, err
	}
	if !compatibleConfiguration(value.Health) || value.Values == nil || len(value.AvailableSites) == 0 {
		return Configuration{}, domain.E(domain.KindUpstream, "桥接不支持当前网页配置，请更新配套版本", nil)
	}
	for _, name := range []string{"proxy_url", "javdb_cookie", "javbus_cookie", "fantia_cookie", "deepl_key", "baidu_secret", "baidu_key", "llm_key"} {
		delete(value.Values, name)
	}
	value.ConnectionGeneration = cfg.Generation
	return value, nil
}

func compatibleConfiguration(health Health) bool {
	for _, capability := range RequiredCapabilities {
		if !slices.Contains(health.Capabilities, capability) {
			return false
		}
	}
	return health.BridgeVersion == BridgeVersion && health.Protocol == Protocol && health.Revision == EngineRevision && health.ConfigurationVersion == ConfigurationVersion && health.ConfigurationID != ""
}
