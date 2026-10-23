// Package mdcz owns the optional, versioned boundary to the MDCz sidecar.
// Upstream runtime changes belong in extensions/mdcz-bridge/adapter.ts.
package mdcz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/atomicfile"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/metadata"
)

const Protocol = 1
const EngineRevision = "f65e7d4161151aff1531f3b99872e49234d5d539"
const imagePrefix = "mdcz:v1/"

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Settings struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	APIKey    string `json:"api_key,omitempty"`
	HasAPIKey bool   `json:"has_api_key"`
	Translate bool   `json:"translate"`
	Badges    bool   `json:"badges"`
	Position  string `json:"position"`
}

type Update struct {
	Settings
	// Omission preserves the saved secret; an explicit empty string clears it.
	APIKey *string `json:"api_key"`
}

type Health struct {
	Protocol             int    `json:"protocol"`
	Revision             string `json:"revision"`
	ConfigurationVersion int    `json:"configuration_version"`
	ConfigurationID      string `json:"configuration_id"`
}

type Service struct {
	*metadata.Service
	db        *ent.Client
	directory string
	client    *http.Client
	mu        sync.RWMutex
	settings  Settings
	slots     chan struct{}
	catalogue func(context.Context, domain.MovieRef) (domain.MovieMetadata, error)
}

func New(ctx context.Context, db *ent.Client, dataDir string, base *metadata.Service) (*Service, error) {
	s := &Service{Service: base, db: db, directory: filepath.Join(dataDir, "extensions", "mdcz-v1"),
		settings: Settings{Position: "bottomLeft", Badges: true}, slots: make(chan struct{}, 1), catalogue: base.Fallback}
	value, found, err := database.LoadSetting[Settings](ctx, db, "extensions.mdcz.v1")
	if err != nil {
		return nil, err
	}
	if found {
		s.settings = value
	}
	if err := os.MkdirAll(s.directory, 0o700); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // The sidecar has its own source-site proxy configuration.
	s.client = &http.Client{Transport: transport, Timeout: 3 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	context.AfterFunc(ctx, s.client.CloseIdleConnections)
	return s, nil
}

func (s *Service) config() Settings { s.mu.RLock(); defer s.mu.RUnlock(); return s.settings }
func (s *Service) ExtensionSettings() Settings {
	cfg := s.config()
	cfg.HasAPIKey, cfg.APIKey = cfg.APIKey != "", ""
	return cfg
}

func validate(cfg Settings) error {
	switch cfg.Position {
	case "topLeft", "topRight", "bottomLeft", "bottomRight":
	default:
		return domain.E(domain.KindInvalid, "请选择有效的角标位置", nil)
	}
	if cfg.ServerURL != "" {
		u, err := url.Parse(cfg.ServerURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return domain.E(domain.KindInvalid, "桥接地址需为 HTTP(S) 服务根地址，不能含账号或参数", nil)
		}
	}
	if strings.ContainsAny(cfg.APIKey, "\r\n") || len(cfg.APIKey) > 4096 {
		return domain.E(domain.KindInvalid, "桥接密钥格式无效", nil)
	}
	if cfg.Enabled && (cfg.ServerURL == "" || cfg.APIKey == "") {
		return domain.E(domain.KindInvalid, "启用前请填写桥接服务地址和密钥", nil)
	}
	if cfg.Enabled && len(cfg.APIKey) < 24 {
		return domain.E(domain.KindInvalid, "桥接密钥至少需要 24 个字符", nil)
	}
	return nil
}

func (s *Service) candidate(change Update) Settings {
	cfg := change.Settings
	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	cfg.APIKey = s.config().APIKey
	if change.APIKey != nil {
		cfg.APIKey = strings.TrimSpace(*change.APIKey)
	}
	cfg.HasAPIKey = false
	return cfg
}

func (s *Service) UpdateExtensionSettings(ctx context.Context, change Update) error {
	cfg := s.candidate(change)
	if err := validate(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := database.SaveSetting(ctx, s.db, "extensions.mdcz.v1", cfg); err != nil {
		return err
	}
	s.settings = cfg
	return nil
}

func (s *Service) Test(ctx context.Context, change Update) (Health, error) {
	cfg := s.candidate(change)
	cfg.Enabled = true
	if err := validate(cfg); err != nil {
		return Health{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var health Health
	err := s.json(ctx, cfg, http.MethodGet, "/v1/health", nil, &health)
	if err == nil && !compatibleConfiguration(health) {
		err = domain.E(domain.KindUpstream, "桥接版本不匹配，请使用文档中的固定版本", nil)
	}
	return health, err
}

// Resolve is injected only into the library scrape pipeline. Browsing never
// triggers a second scraper and the existing source-settings API stays intact.
func (s *Service) Resolve(ctx context.Context, ref domain.MovieRef) (domain.MovieMetadata, error) {
	cfg := s.config()
	if !cfg.Enabled {
		return s.Service.Resolve(ctx, ref)
	}
	result, err := s.resolve(ctx, cfg, ref)
	if err != nil {
		return domain.MovieMetadata{}, err
	}
	// Reuse only the native exact-code JavDB lookup, preserving catalogue links
	// and the original rating scale without running all native scrapers again.
	catalogue, err := s.catalogue(ctx, ref)
	if ctx.Err() != nil {
		return domain.MovieMetadata{}, ctx.Err()
	}
	if err != nil {
		if domain.IsKind(err, domain.KindConflict) || (ref.Refresh && !errors.Is(err, metadata.ErrNotFound)) {
			return domain.MovieMetadata{}, err
		}
		return result, nil
	}
	detail := catalogue.Detail
	if !codeid.IsFormatEquivalent(ref.Code, detail.Code) || (ref.JavDBID != "" && ref.JavDBID != detail.ID) {
		return domain.MovieMetadata{}, domain.E(domain.KindConflict, "JavDB 身份与 MDCz 资料不一致", nil)
	}
	result.Detail.ID = detail.ID
	result.Detail.Sources = []domain.SourceID{{Provider: "mdcz", ID: result.Detail.Code}}
	if detail.ID != "" {
		result.Detail.Sources = append(result.Detail.Sources, domain.SourceID{Provider: "javdb", ID: detail.ID})
	}
	result.Detail.Rating, result.Detail.RatingSource, result.Detail.RatingMax = detail.Rating, detail.RatingSource, detail.RatingMax
	return result, nil
}

// Synopsis uses this bridge-only path: opening a detail does not perform a
// second catalogue lookup or run the native artwork pipeline.
func (s *Service) resolve(ctx context.Context, cfg Settings, ref domain.MovieRef) (domain.MovieMetadata, error) {
	var result struct {
		Health
		Partial bool                    `json:"partial"`
		Detail  domain.MovieDetail      `json:"detail"`
		Images  []domain.ImageCandidate `json:"images"`
	}
	if err := s.json(ctx, cfg, http.MethodPost, "/v1/metadata", map[string]any{"code": ref.Code, "translate": cfg.Translate}, &result); err != nil {
		return domain.MovieMetadata{}, err
	}
	if result.Protocol != Protocol || result.Revision != EngineRevision || !codeid.IsFormatEquivalent(ref.Code, result.Detail.Code) || strings.TrimSpace(result.Detail.Title) == "" {
		return domain.MovieMetadata{}, domain.E(domain.KindUpstream, "MDCz 返回的影片身份或协议版本不匹配", nil)
	}
	if ref.Refresh && result.Partial {
		return domain.MovieMetadata{}, domain.E(domain.KindUpstream, "MDCz 部分来源未完成，暂不覆盖已有资料，请稍后重试", nil)
	}
	for _, candidate := range result.Images {
		if candidate.Provider != "mdcz" || !validImageID(candidate.URL) {
			return domain.MovieMetadata{}, domain.E(domain.KindUpstream, "MDCz 图片引用无效", nil)
		}
	}
	result.Detail.Code = codeid.Normalize(ref.Code)
	result.Detail.ID = ref.JavDBID
	result.Detail.Sources = []domain.SourceID{{Provider: "mdcz", ID: result.Detail.Code}}
	if ref.JavDBID != "" {
		result.Detail.Sources = append(result.Detail.Sources, domain.SourceID{Provider: "javdb", ID: ref.JavDBID})
	}
	if result.Detail.Plot != "" {
		body, err := json.Marshal(Synopsis{Enabled: true, Plot: result.Detail.Plot, ConfigurationID: result.ConfigurationID})
		if err != nil {
			return domain.MovieMetadata{}, err
		}
		if err := atomicfile.WriteFile(s.synopsisPath(cfg, ref.Code), body, 0o600); err != nil {
			return domain.MovieMetadata{}, err
		}
	}
	return domain.MovieMetadata{Detail: result.Detail, Images: result.Images}, nil
}

func validImageID(value string) bool {
	return strings.HasPrefix(value, imagePrefix) && hashPattern.MatchString(strings.TrimPrefix(value, imagePrefix))
}

func (s *Service) Image(ctx context.Context, candidate domain.ImageCandidate) (domain.Media, error) {
	if candidate.Provider != "mdcz" {
		return s.Service.Image(ctx, candidate)
	}
	if !validImageID(candidate.URL) {
		return domain.Media{}, domain.E(domain.KindInvalid, "无效的 MDCz 图片引用", nil)
	}
	body, err := s.request(ctx, s.config(), http.MethodGet, "/v1/images/"+strings.TrimPrefix(candidate.URL, imagePrefix), nil, 20<<20)
	return domain.Media{Body: body, ContentType: http.DetectContentType(body)}, err
}

func (s *Service) json(ctx context.Context, cfg Settings, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	body, err = s.request(ctx, cfg, method, path, body, 32<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, output); err != nil {
		return domain.E(domain.KindUpstream, "桥接服务返回了无效数据", err)
	}
	return nil
}

func (s *Service) request(ctx context.Context, cfg Settings, method, path string, body []byte, limit int64) ([]byte, error) {
	if cfg.ServerURL == "" || cfg.APIKey == "" {
		return nil, domain.E(domain.KindInvalid, "请先配置 MDCz 桥接服务", nil)
	}
	if path != "/v1/health" && !(path == "/v1/configuration" && method == http.MethodGet) {
		select {
		case s.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { <-s.slots }()
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.ServerURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.E(domain.KindUpstream, "MDCz 桥接服务不可达或超时，请检查连接后重试", nil)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if path == "/v1/configuration" {
			switch response.StatusCode {
			case http.StatusBadRequest:
				return nil, domain.E(domain.KindInvalid, "MDCz 配置无效，请检查来源、代理地址和翻译参数", nil)
			case http.StatusConflict:
				return nil, domain.E(domain.KindConflict, "配置已被修改，请重新读取后再保存", nil)
			case http.StatusNotFound:
				return nil, domain.E(domain.KindUpstream, "桥接不支持网页配置，请更新配套版本", nil)
			case http.StatusServiceUnavailable:
				return nil, domain.E(domain.KindConflict, "桥接正在刮削，请等待当前任务完成后重试", nil)
			}
		}
		// Do not relay upstream response bodies: site errors can contain credentials.
		return nil, domain.E(domain.KindUpstream, fmt.Sprintf("MDCz 桥接请求失败（HTTP %d），请检查配置后重试", response.StatusCode), nil)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, domain.E(domain.KindUpstream, "MDCz 返回数据过大", nil)
	}
	return data, nil
}

type Synopsis struct {
	Enabled         bool   `json:"enabled"`
	Plot            string `json:"plot"`
	ConfigurationID string `json:"configuration_id,omitempty"`
}

func (s *Service) synopsisPath(cfg Settings, code string) string {
	key, _ := json.Marshal([]any{EngineRevision, cfg.ServerURL, cfg.Translate, codeid.Normalize(code)})
	return filepath.Join(s.directory, "synopsis-"+digest(key)+".json")
}

func (s *Service) Synopsis(ctx context.Context, code string) (Synopsis, error) {
	cfg := s.config()
	var health Health
	if cfg.Enabled {
		// A lightweight health read prevents a changed source/translation profile
		// from reusing an introduction produced by the previous configuration.
		if err := s.json(ctx, cfg, http.MethodGet, "/v1/health", nil, &health); err != nil {
			return Synopsis{}, err
		}
		if !compatibleConfiguration(health) {
			return Synopsis{}, domain.E(domain.KindUpstream, "桥接配置版本不匹配，请更新配套版本", nil)
		}
	}
	result := Synopsis{Enabled: cfg.Enabled}
	body, err := os.ReadFile(s.synopsisPath(cfg, code))
	if err == nil && json.Unmarshal(body, &result) == nil && result.Plot != "" && (!cfg.Enabled || result.ConfigurationID == health.ConfigurationID) {
		result.Enabled = cfg.Enabled
		return result, nil
	}
	if !cfg.Enabled {
		return Synopsis{}, nil
	}
	result = Synopsis{Enabled: true}
	metadata, err := s.resolve(ctx, cfg, domain.MovieRef{Code: code})
	if err != nil {
		return result, err
	}
	result.Plot = metadata.Detail.Plot
	return result, nil
}
