package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/emby"
)

// EmbyPlaybackManager is optional; existing settings/notification fakes do not
// need to implement playback resolution or change their response contracts.
type EmbyPlaybackManager interface {
	PlaybackTarget(context.Context, string) (emby.PlaybackTarget, error)
}

func registerEmbyPlayback(protected *gin.RouterGroup, manager EmbyPlaybackManager) {
	if manager == nil {
		return
	}
	protected.GET("/extensions/emby/playback", noStore(), func(c *gin.Context) {
		q, ok := bindQuery[struct {
			Code string `form:"code" binding:"required,max=200"`
		}](c)
		if !ok {
			return
		}
		target, err := manager.PlaybackTarget(c.Request.Context(), q.Code)
		respond(c, target, err)
	})
}
