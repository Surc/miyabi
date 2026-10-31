package top

import (
	"context"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
)

const configSetting = "top.config"

type Config struct {
	ShowNavigation bool   `json:"show_navigation"`
	ShowRankings   bool   `json:"show_rankings"`
	DefaultBoard   string `json:"default_board"`
}

func DefaultConfig() Config {
	return Config{ShowNavigation: true, ShowRankings: true, DefaultBoard: "all"}
}

func (config Config) Validate() error {
	if _, found := load().boards[config.DefaultBoard]; !found {
		return domain.E(domain.KindInvalid, "默认榜单不存在", nil)
	}
	return nil
}

type Service struct {
	db *ent.Client
}

func NewService(db *ent.Client) *Service { return &Service{db: db} }

func (service *Service) Config(ctx context.Context) (Config, error) {
	config, found, err := database.LoadSetting[Config](ctx, service.db, configSetting)
	if err != nil {
		return Config{}, err
	}
	if !found {
		return DefaultConfig(), nil
	}
	return config, config.Validate()
}

func (service *Service) UpdateConfig(ctx context.Context, config Config) (Config, error) {
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	if err := database.SaveSetting(ctx, service.db, configSetting, config); err != nil {
		return Config{}, err
	}
	return config, nil
}
