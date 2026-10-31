package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/top"
)

type TopSettingsManager interface {
	Config(context.Context) (top.Config, error)
	UpdateConfig(context.Context, top.Config) (top.Config, error)
}

func registerTop(protected *gin.RouterGroup, settings TopSettingsManager) {
	group := protected.Group("/top")
	group.GET("", func(c *gin.Context) { respond(c, top.Metadata(), nil) })
	group.GET("/movies", func(c *gin.Context) {
		query, ok := bindQuery[struct {
			Board string `form:"board,default=all" binding:"max=20"`
			Page  int    `form:"page,default=1" binding:"min=1,max=1000000"`
			Limit int    `form:"limit,default=48" binding:"min=1,max=100"`
		}](c)
		if !ok {
			return
		}
		page, found := top.Movies(query.Board, (query.Page-1)*query.Limit, query.Limit)
		if !found {
			respond(c, nil, domain.E(domain.KindNotFound, "榜单不存在", nil))
			return
		}
		respond(c, page, nil)
	})
	group.GET("/rankings", func(c *gin.Context) {
		query, ok := bindQuery[struct {
			Code string `form:"code" binding:"required,max=200"`
		}](c)
		if !ok {
			return
		}
		code := codeid.Normalize(query.Code)
		if code == "" {
			respond(c, nil, domain.E(domain.KindInvalid, "影片番号不能为空", nil))
			return
		}
		respond(c, top.Rankings(code), nil)
	})
	if settings == nil {
		return
	}
	configuration := protected.Group("/settings/top", noStore())
	configuration.GET("", func(c *gin.Context) {
		config, err := settings.Config(c.Request.Context())
		respond(c, config, err)
	})
	configuration.PUT("", func(c *gin.Context) {
		config, ok := bindJSON[top.Config](c)
		if !ok {
			return
		}
		saved, err := settings.UpdateConfig(c.Request.Context(), config)
		respond(c, saved, err)
	})
}
