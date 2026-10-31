package api

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/domain"
)

func registerBrowseArchive(group *gin.RouterGroup, service BrowseExtensionManager) {
	group.GET("/cache/export", func(c *gin.Context) {
		file, err := os.CreateTemp("", "miyabi-cache-export-*.zip")
		if err != nil {
			c.Error(err)
			return
		}
		defer os.Remove(file.Name())
		defer file.Close()
		if err := service.ExportCache(c.Request.Context(), file); err != nil {
			c.Error(err)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			c.Error(err)
			return
		}
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="miyabi-cache-`+time.Now().UTC().Format("2006-01-02")+`.zip"`)
		http.ServeContent(c.Writer, c.Request, "cache.zip", time.Time{}, file)
	})
	group.POST("/cache/import", func(c *gin.Context) {
		file, err := os.CreateTemp("", "miyabi-cache-import-*.zip")
		if err != nil {
			c.Error(err)
			return
		}
		defer os.Remove(file.Name())
		defer file.Close()
		body := http.MaxBytesReader(c.Writer, c.Request.Body, catalogue.BrowseArchiveMaxBytes)
		size, err := io.Copy(file, body)
		if err != nil {
			respond(c, nil, domain.E(domain.KindInvalid, "缓存文件读取失败，文件不能超过 512 MB", err))
			return
		}
		stats, err := service.ImportCache(c.Request.Context(), file, size)
		respond(c, stats, err)
	})
}
