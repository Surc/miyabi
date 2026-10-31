package subtitle

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
)

const configSetting = "subtitle.config"

type Sources struct {
	Xunlei      bool `json:"xunlei"`
	SubtitleCat bool `json:"subtitlecat"`
	AVSubtitles bool `json:"avsubtitles"`
}

func (s Sources) Any() bool { return s.Xunlei || s.SubtitleCat || s.AVSubtitles }

func (s Sources) enabled(id string) bool {
	switch id {
	case "xunlei":
		return s.Xunlei
	case "subtitlecat":
		return s.SubtitleCat
	case "avsubtitles":
		return s.AVSubtitles
	default:
		return true // Explicitly injected providers, such as test fixtures.
	}
}

type Config struct {
	Enabled  bool                  `json:"enabled"`
	Sources  Sources               `json:"sources"`
	Language subtitlemeta.Language `json:"language"`
}

func DefaultConfig() Config {
	return Config{
		Enabled: true, Sources: Sources{Xunlei: true, SubtitleCat: true, AVSubtitles: true},
		Language: subtitlemeta.LangSimplifiedChinese,
	}
}

func (c *Config) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return domain.E(domain.KindInvalid, "字幕下载配置不能为空", nil)
	}
	type plain Config
	value := plain(DefaultConfig())
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = Config(value)
	return nil
}

func (c Config) Validate() error {
	if c.Language != subtitlemeta.LangSimplifiedChinese && c.Language != subtitlemeta.LangTraditionalChinese {
		return domain.E(domain.KindInvalid, "字幕语言偏好必须为简体或繁体", nil)
	}
	return nil
}

func (s *Service) Config(ctx context.Context) (Config, error) {
	config, found, err := database.LoadSetting[Config](ctx, s.db, configSetting)
	if err != nil {
		return Config{}, err
	}
	if !found {
		return DefaultConfig(), nil
	}
	return config, config.Validate()
}

func (s *Service) UpdateConfig(ctx context.Context, config Config) (Config, error) {
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	if err := database.SaveSetting(ctx, s.db, configSetting, config); err != nil {
		return Config{}, err
	}
	return config, nil
}
