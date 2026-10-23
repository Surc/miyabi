package mdcz

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ppxb/miyabi/internal/domain"
)

const ConfigurationVersion = 1

// The bridge owns validation/defaults and durable configuration. Miyabi never
// stores a second copy or exposes secret values returned by a faulty bridge.
type Configuration struct {
	Health
	Values         map[string]json.RawMessage `json:"values"`
	Secrets        map[string]bool            `json:"secrets"`
	AvailableSites []string                   `json:"available_sites"`
}

type ConfigurationUpdate struct {
	Version int                        `json:"configuration_version"`
	ID      string                     `json:"configuration_id"`
	Values  map[string]json.RawMessage `json:"values"`
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
	if err := s.json(ctx, s.config(), method, "/v1/configuration", input, &value); err != nil {
		return Configuration{}, err
	}
	if !compatibleConfiguration(value.Health) || value.Values == nil || len(value.AvailableSites) == 0 {
		return Configuration{}, domain.E(domain.KindUpstream, "桥接不支持当前网页配置，请更新配套版本", nil)
	}
	for _, name := range []string{"proxy_url", "javdb_cookie", "javbus_cookie", "fantia_cookie", "deepl_key", "baidu_secret", "baidu_key", "llm_key"} {
		delete(value.Values, name)
	}
	return value, nil
}

func compatibleConfiguration(health Health) bool {
	return health.Protocol == Protocol && health.Revision == EngineRevision && health.ConfigurationVersion == ConfigurationVersion && health.ConfigurationID != ""
}
