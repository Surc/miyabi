package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/subtitle"
)

type SubtitleManager interface {
	Config(context.Context) (subtitle.Config, error)
	UpdateConfig(context.Context, subtitle.Config) (subtitle.Config, error)
	TestDownload(context.Context, subtitle.TestRequest) (subtitle.TestResult, error)
}

func subtitleSettingsHandler(service SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := service.Config(c.Request.Context())
		respond(c, config, err)
	}
}

func subtitleSettingsUpdateHandler(service SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, ok := bindJSON[subtitle.Config](c)
		if !ok {
			return
		}
		saved, err := service.UpdateConfig(c.Request.Context(), config)
		respond(c, saved, err)
	}
}

func subtitleTestHandler(service SubtitleManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		request, ok := bindJSON[subtitle.TestRequest](c)
		if !ok {
			return
		}
		result, err := service.TestDownload(c.Request.Context(), request)
		respond(c, result, err)
	}
}
