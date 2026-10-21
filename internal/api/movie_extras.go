package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

// MovieExtrasManager is intentionally separate from CatalogueManager.
type MovieExtrasManager interface {
	JavLibraryRating(context.Context, string) (domain.ExternalRating, error)
	MovieComments(context.Context, string, string, string, int) (domain.MovieComments, error)
}

func registerMovieExtras(protected *gin.RouterGroup, extras MovieExtrasManager) {
	if extras == nil {
		return
	}
	group := protected.Group("/extensions/movie", noStore())
	group.GET("/javlibrary-rating", func(c *gin.Context) {
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
		rating, err := extras.JavLibraryRating(c.Request.Context(), code)
		respond(c, rating, err)
	})
	group.GET("/comments", func(c *gin.Context) {
		q, ok := bindQuery[struct {
			Source string `form:"source" binding:"required,oneof=javdb javlibrary"`
			ID     string `form:"javdb_id" binding:"max=200"`
			Code   string `form:"code" binding:"max=200"`
			Page   int    `form:"page,default=1" binding:"min=1,max=1000"`
		}](c)
		if !ok {
			return
		}
		code := codeid.Normalize(q.Code)
		if (q.Source == "javdb" && q.ID == "") || (q.Source == "javlibrary" && code == "") {
			respond(c, nil, domain.E(domain.KindInvalid, "缺少影片标识", nil))
			return
		}
		comments, err := extras.MovieComments(c.Request.Context(), q.Source, q.ID, code, q.Page)
		respond(c, comments, err)
	})
}
