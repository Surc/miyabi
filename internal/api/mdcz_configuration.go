package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/mdcz"
)

// Optional capability keeps the original MDCzManager contract unchanged.
type mdczConfigurationManager interface {
	Configuration(context.Context) (mdcz.Configuration, error)
	UpdateConfiguration(context.Context, mdcz.ConfigurationUpdate) (mdcz.Configuration, error)
}

func registerMDCzConfiguration(group *gin.RouterGroup, service MDCzManager) {
	manager, ok := service.(mdczConfigurationManager)
	if !ok {
		return
	}
	group.GET("/configuration", func(c *gin.Context) {
		value, err := manager.Configuration(c.Request.Context())
		respond(c, value, err)
	})
	group.PUT("/configuration", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
		input, ok := bindJSON[mdcz.ConfigurationUpdate](c)
		if !ok {
			return
		}
		value, err := manager.UpdateConfiguration(c.Request.Context(), input)
		respond(c, value, err)
	})
}
