package api

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

// BrowseExtensionManager adds durable browsing without widening CatalogueManager.
type BrowseExtensionManager interface {
	BrowsePage(context.Context, domain.BrowseOptions, bool) (catalogue.BrowseCached[[]catalogue.Movie], error)
	SearchPage(context.Context, string, domain.SearchOptions, bool) (catalogue.BrowseCached[[]catalogue.Movie], error)
	Resources(context.Context, string, bool) (catalogue.BrowseCached[catalogue.BrowseResources], error)
	Rating(context.Context, string, string, bool) (catalogue.BrowseCached[domain.ExternalRating], error)
	RatingByCode(context.Context, string, bool) (catalogue.BrowseCached[domain.ExternalRating], error)
	CachedRating(string, string) *catalogue.BrowseCached[domain.ExternalRating]
	Tags(context.Context, domain.Zone, bool) (catalogue.BrowseCached[[]domain.TagCategory], error)
	Media(context.Context, string) (domain.Media, error)
	PreviewImage(context.Context, domain.ImageCandidate, func(context.Context, domain.ImageCandidate) (domain.Media, error)) (domain.Media, error)
	ExportCache(context.Context, io.Writer) error
	ImportCache(context.Context, io.ReaderAt, int64) (catalogue.BrowseCacheStats, error)
	Image(context.Context, string) (domain.Media, error)
	Settings() catalogue.BrowseCacheSettings
	UpdateSettings(catalogue.BrowseCacheSettings) (catalogue.BrowseCacheSettings, error)
	CacheStats() (catalogue.BrowseCacheStats, error)
	ClearCache(string) (catalogue.BrowseCacheStats, error)
}

func registerBrowseExtension(protected *gin.RouterGroup, service BrowseExtensionManager) {
	if service == nil {
		return
	}
	group := protected.Group("/extensions/browse", noStore())
	registerBrowseArchive(group, service)
	group.GET("/tags", func(c *gin.Context) {
		q, ok := bindQuery[discoverTagsQuery](c)
		if !ok {
			return
		}
		refresh, ok := browseRefresh(c)
		if !ok {
			return
		}
		result, err := service.Tags(c.Request.Context(), domain.Zone(q.Zone), refresh)
		respond(c, result, err)
	})
	group.GET("/ratings/javlibrary", func(c *gin.Context) {
		q, ok := bindQuery[struct {
			Code string `form:"code" binding:"required,max=200"`
		}](c)
		if !ok {
			return
		}
		code := codeid.Normalize(q.Code)
		if code == "" {
			respond(c, nil, domain.E(domain.KindInvalid, "影片番号不能为空", nil))
			return
		}
		refresh, ok := browseRefresh(c)
		if !ok {
			return
		}
		cachedOnly, ok := browseFlag(c, "cached_only")
		if !ok {
			return
		}
		if cachedOnly {
			respond(c, service.CachedRating("javlibrary-code", code), nil)
			return
		}
		result, err := service.RatingByCode(c.Request.Context(), code, refresh)
		respond(c, result, err)
	})
	group.GET("/movies", func(c *gin.Context) {
		q, ok := bindQuery[discoverBrowseQuery](c)
		if !ok {
			return
		}
		refresh, ok := browseRefresh(c)
		if !ok {
			return
		}
		result, err := service.BrowsePage(c.Request.Context(), domain.BrowseOptions{Zone: domain.Zone(q.Zone), EntityType: domain.EntityType(q.EntityType), EntityID: q.EntityID, Main: q.Main, TagIDs: q.TagIDs, Year: q.Year, Month: q.Month, Sort: q.Sort, Order: q.Order, Page: q.Page, Limit: q.Limit}, refresh)
		respond(c, result, err)
	})
	group.GET("/search", func(c *gin.Context) {
		q, ok := bindQuery[discoverSearchQuery](c)
		if !ok {
			return
		}
		refresh, ok := browseRefresh(c)
		if !ok {
			return
		}
		result, err := service.SearchPage(c.Request.Context(), q.Query, domain.SearchOptions{Page: q.Page, Limit: q.Limit}, refresh)
		respond(c, result, err)
	})
	group.GET("/movies/:id/resources", func(c *gin.Context) {
		id, ok := browseID(c)
		if !ok {
			return
		}
		refresh, ok := browseRefresh(c)
		if !ok {
			return
		}
		result, err := service.Resources(c.Request.Context(), id, refresh)
		respond(c, result, err)
	})
	group.GET("/movies/:id/ratings/:source", func(c *gin.Context) {
		id, ok := browseID(c)
		if !ok {
			return
		}
		refresh, ok := browseRefresh(c)
		if !ok {
			return
		}
		source := c.Param("source")
		if source != "javdb" && source != "javlibrary" {
			respond(c, nil, domain.E(domain.KindInvalid, "不支持的评分来源", nil))
			return
		}
		cachedOnly, ok := browseFlag(c, "cached_only")
		if !ok {
			return
		}
		if cachedOnly {
			respond(c, service.CachedRating(source, id), nil)
			return
		}
		result, err := service.Rating(c.Request.Context(), source, id, refresh)
		respond(c, result, err)
	})
	group.GET("/movies/:id/image", func(c *gin.Context) {
		id, ok := browseID(c)
		if !ok {
			return
		}
		media, err := service.Image(c.Request.Context(), id)
		if err != nil {
			c.Error(err)
			return
		}
		c.Header("Cache-Control", "private, max-age=300")
		c.Writer.Header().Add("Vary", "Cookie")
		c.Data(http.StatusOK, media.ContentType, media.Body)
	})
	group.GET("/settings", func(c *gin.Context) { respond(c, service.Settings(), nil) })
	group.PUT("/settings", func(c *gin.Context) {
		settings, ok := bindJSON[catalogue.BrowseCacheSettings](c)
		if !ok {
			return
		}
		result, err := service.UpdateSettings(settings)
		respond(c, result, err)
	})
	group.GET("/cache", func(c *gin.Context) { result, err := service.CacheStats(); respond(c, result, err) })
	group.DELETE("/cache", func(c *gin.Context) {
		result, err := service.ClearCache(c.DefaultQuery("scope", "all"))
		respond(c, result, err)
	})
}
func browseID(c *gin.Context) (string, bool) {
	var uri struct {
		ID string `uri:"id" binding:"required,max=200"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		respond(c, nil, domain.E(domain.KindInvalid, "无效的作品标识", nil))
		return "", false
	}
	return uri.ID, true
}
func browseRefresh(c *gin.Context) (bool, bool) {
	return browseFlag(c, "refresh")
}
func browseFlag(c *gin.Context, name string) (bool, bool) {
	value := c.Query(name)
	if value != "" && value != "0" && value != "1" {
		respond(c, nil, domain.E(domain.KindInvalid, "无效的刷新参数", nil))
		return false, false
	}
	return value == "1", true
}
