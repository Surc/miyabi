package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type javdbSessionManager interface {
	JavDBSessionConfigured() bool
	LoginJavDB(context.Context, string, string) error
	LogoutJavDB(context.Context) error
}

func registerJavDBSession(settings *gin.RouterGroup, catalogue CatalogueManager) {
	service, ok := catalogue.(javdbSessionManager)
	if !ok {
		return
	}
	group := settings.Group("/javdb/session")
	group.GET("", func(c *gin.Context) {
		respond(c, gin.H{"configured": service.JavDBSessionConfigured()}, nil)
	})
	group.POST("", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		variables, ok := bindJSON[struct {
			Username string `json:"username" binding:"required,max=254"`
			Password string `json:"password" binding:"required,max=1024"`
		}](c)
		if !ok {
			return
		}
		err := service.LoginJavDB(c.Request.Context(), variables.Username, variables.Password)
		respond(c, gin.H{"configured": true}, err)
	})
	group.DELETE("", func(c *gin.Context) {
		err := service.LogoutJavDB(c.Request.Context())
		respond(c, gin.H{"configured": false}, err)
	})
}
