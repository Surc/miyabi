package api

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/library"
)

type librarySubtitleManager interface {
	MovieSubtitles(context.Context, int) ([]library.SubtitleVideo, error)
	DownloadMovieSubtitles(context.Context, int, int) (int, error)
	AddMovieSubtitle(context.Context, int, int, string, string, []byte) error
	DeleteMovieSubtitle(context.Context, int, int, string) error
}

type subtitleVideoURI struct {
	ID      int `uri:"id" binding:"min=1"`
	VideoID int `uri:"video" binding:"min=1"`
}

func registerLibrarySubtitles(protected *gin.RouterGroup, libraryManager LibraryManager) {
	manager, ok := libraryManager.(librarySubtitleManager)
	if !ok {
		return
	}
	routes := protected.Group("/library/movies/:id/subtitles", noStore())
	routes.GET("", func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"min=1"`
		}](c)
		if ok {
			videos, err := manager.MovieSubtitles(c.Request.Context(), uri.ID)
			respond(c, videos, err)
		}
	})
	routes.POST("/:video/download", func(c *gin.Context) {
		uri, ok := bindURI[subtitleVideoURI](c)
		if ok {
			count, err := manager.DownloadMovieSubtitles(c.Request.Context(), uri.ID, uri.VideoID)
			respond(c, gin.H{"downloaded": count}, err)
		}
	})
	routes.POST("/:video", func(c *gin.Context) {
		uri, ok := bindURI[subtitleVideoURI](c)
		if !ok {
			return
		}
		query, ok := bindQuery[struct {
			Name     string `form:"name" binding:"required,max=255"`
			Language string `form:"language" binding:"max=8"`
		}](c)
		if !ok {
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, subtitlemeta.MaxFileSize))
		if err != nil {
			c.Error(domain.E(domain.KindInvalid, "无法读取字幕，文件大小上限为 10 MiB", err))
			return
		}
		err = manager.AddMovieSubtitle(c.Request.Context(), uri.ID, uri.VideoID, query.Name, query.Language, body)
		created(c, gin.H{"ok": true}, err)
	})
	routes.DELETE("/:video/:track", func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID      int    `uri:"id" binding:"min=1"`
			VideoID int    `uri:"video" binding:"min=1"`
			Track   string `uri:"track" binding:"required,len=64,hexadecimal"`
		}](c)
		if ok {
			err := manager.DeleteMovieSubtitle(c.Request.Context(), uri.ID, uri.VideoID, uri.Track)
			respond(c, gin.H{"ok": true}, err)
		}
	})
}
