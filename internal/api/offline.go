package api

import (
	"context"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/offline"
)

type OfflineManager interface {
	Add(context.Context, string, string) (domain.OfflineSubmission, error)
	AddED2K(context.Context, string, string, int) (domain.OfflineSubmission, error)
	Activity(context.Context) (offline.Activity, error)
	Cancel(context.Context, int) (domain.OfflineSubmission, error)
	TryNext(context.Context, int) (domain.OfflineSubmission, error)
}

func offlineControlHandler(manager OfflineManager, next bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[struct {
			ID int `uri:"id" binding:"required,min=1"`
		}](c)
		if !ok {
			return
		}
		var result domain.OfflineSubmission
		var err error
		if next {
			result, err = manager.TryNext(c.Request.Context(), uri.ID)
		} else {
			result, err = manager.Cancel(c.Request.Context(), uri.ID)
		}
		accepted(c, result, err)
	}
}

func offlineActivityHandler(offline OfflineManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		activity, err := offline.Activity(c.Request.Context())
		respond(c, activity, err)
	}
}

type offlineInput struct {
	Hash string `json:"hash" binding:"omitempty,len=40,hexadecimal"`
	ED2K string `json:"ed2k" binding:"max=57"`
	Page int    `json:"page" binding:"omitempty,min=1,max=1000"`
}

var ed2kResourceID = regexp.MustCompile(`^ed2k:[a-f0-9]{32}:[1-9][0-9]{0,18}$`)

func offlineAddHandler(offline OfflineManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		uri, ok := bindURI[movieURI](c)
		if !ok {
			return
		}
		input, ok := bindJSON[offlineInput](c)
		if !ok {
			return
		}
		if (input.Hash == "") == (input.ED2K == "") {
			accepted(c, nil, domain.E(domain.KindInvalid, "请选择一条下载资源", nil))
			return
		}
		if input.ED2K != "" {
			if !ed2kResourceID.MatchString(input.ED2K) {
				accepted(c, nil, domain.E(domain.KindInvalid, "无效的 ED2K 资源标识", nil))
				return
			}
			submission, err := offline.AddED2K(c.Request.Context(), uri.ID, input.ED2K, max(input.Page, 1))
			accepted(c, submission, err)
			return
		}
		submission, err := offline.Add(c.Request.Context(), uri.ID, input.Hash)
		accepted(c, submission, err)
	}
}
