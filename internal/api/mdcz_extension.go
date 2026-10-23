package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/mdcz"
)

type MDCzManager interface {
	ExtensionSettings() mdcz.Settings
	UpdateExtensionSettings(context.Context, mdcz.Update) error
	Test(context.Context, mdcz.Update) (mdcz.Health, error)
	Synopsis(context.Context, string) (mdcz.Synopsis, error)
}

func registerMDCz(protected *gin.RouterGroup, service MDCzManager) {
	if service == nil {
		return
	}
	group := protected.Group("/extensions/mdcz", noStore())
	group.GET("/synopsis", func(c *gin.Context) {
		query, ok := bindQuery[struct {
			Code string `form:"code" binding:"required,max=128"`
		}](c)
		if !ok {
			return
		}
		code := codeid.Normalize(query.Code)
		if code == "" {
			c.Error(domain.E(domain.KindInvalid, "请提供影片番号", nil))
			return
		}
		value, err := service.Synopsis(c.Request.Context(), code)
		respond(c, value, err)
	})
	group.GET("/settings", func(c *gin.Context) { respond(c, service.ExtensionSettings(), nil) })
	group.PUT("/settings", func(c *gin.Context) {
		change, ok := bindJSON[mdcz.Update](c)
		if !ok {
			return
		}
		if err := service.UpdateExtensionSettings(c.Request.Context(), change); err != nil {
			c.Error(err)
			return
		}
		respond(c, service.ExtensionSettings(), nil)
	})
	group.POST("/test", func(c *gin.Context) {
		change, ok := bindJSON[mdcz.Update](c)
		if !ok {
			return
		}
		value, err := service.Test(c.Request.Context(), change)
		respond(c, value, err)
	})
}
