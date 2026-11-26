package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/library"
)

type libraryVideoManager interface {
	DeleteMovieVideo(context.Context, int, int) (library.VideoDeletion, error)
	BackfillVideoSources(context.Context) (library.VideoSourceBackfill, error)
}

func registerLibraryVideos(protected *gin.RouterGroup, manager LibraryManager) {
	videos, ok := manager.(libraryVideoManager)
	if !ok {
		return
	}
	protected.POST("/library/video-sources/backfill", noStore(), func(c *gin.Context) {
		result, err := videos.BackfillVideoSources(c.Request.Context())
		respond(c, result, err)
	})
	protected.DELETE("/library/movies/:id/videos/:video", noStore(), func(c *gin.Context) {
		uri, ok := bindURI[subtitleVideoURI](c)
		if ok {
			result, err := videos.DeleteMovieVideo(c.Request.Context(), uri.ID, uri.VideoID)
			respond(c, result, err)
		}
	})
}
