package api

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/javlibrary"
)

func registerJavLibraryVerification(protected *gin.RouterGroup, relay *javlibrary.BrowserRelay, identify gin.HandlerFunc) {
	group := protected.Group("/extensions/movie/javlibrary-browser", noStore(), identify)
	for _, action := range []string{"open", "confirm"} {
		group.POST("/"+action, func(c *gin.Context) {
			if relay == nil {
				respond(c, nil, domain.E(domain.KindConflict, "Miyabi 未启用验证服务主动连接，请在运行验证服务的电脑上打开它的控制页。", nil))
				return
			}
			err := relay.ControlVerification(c.Request.Context(), action)
			switch {
			case errors.Is(err, javlibrary.ErrBrowserDisconnected):
				err = domain.E(domain.KindUpstream, "验证服务未连接。请在当前电脑上启动验证服务，并直连 Miyabi 后重试。", err)
			case errors.Is(err, javlibrary.ErrBrowserControlUnsupported):
				err = domain.E(domain.KindConflict, "验证服务版本过旧，请更新并重启验证服务。", err)
			case errors.Is(err, javlibrary.ErrVerificationRequired):
				err = domain.E(domain.KindConflict, "尚未检测到验证通过。请在专用 Edge 中打开 JavLibrary，完成验证后再确认。", err)
			case err != nil:
				err = domain.E(domain.KindUpstream, "验证操作失败，请重试。", err)
			}
			respond(c, gin.H{"ok": true}, err)
		})
	}
}
