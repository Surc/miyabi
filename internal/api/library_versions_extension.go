package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/library"
)

type libraryVersionReader interface {
	ResourceVersions(context.Context, []int) ([]library.ResourceVersion, error)
}

func registerLibraryVersions(protected *gin.RouterGroup, manager LibraryManager) {
	reader, ok := manager.(libraryVersionReader)
	if !ok {
		return
	}
	protected.POST("/extensions/library/resource-versions", noStore(), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		input, ok := bindJSON[struct {
			IDs []int `json:"ids" binding:"required,min=1,max=100,dive,gt=0"`
		}](c)
		if !ok {
			return
		}
		versions, err := reader.ResourceVersions(c.Request.Context(), input.IDs)
		respond(c, versions, err)
	})
}
