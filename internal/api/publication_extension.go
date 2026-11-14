package api

import (
	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/export"
)

type PublicationStatusReader interface {
	PublicationStatus() export.PublicationStatus
}

func registerPublicationStatus(protected *gin.RouterGroup, service PublicationStatusReader) {
	if service == nil {
		return
	}
	protected.GET("/extensions/publication/status", noStore(), func(c *gin.Context) {
		// This endpoint only reads the last recovery verdict, never writes files or
		// exposes journal paths, generated STRM contents or low-level error bodies.
		respond(c, service.PublicationStatus(), nil)
	})
}
