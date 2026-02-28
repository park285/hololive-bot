package app

import (
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

func registerTemplateRoutes(holoAPI *gin.RouterGroup, handler *server.TemplateAPIHandler) {
	holoAPI.GET("/templates", handler.GetTemplates)
	holoAPI.GET("/templates/:key", handler.GetTemplateByKey)

	// 템플릿 관리 API
	holoAPI.PUT("/templates/:key", handler.UpsertTemplate)
	holoAPI.DELETE("/templates/:key", handler.DeleteTemplateOverride)
	holoAPI.POST("/templates/:key/preview", handler.PreviewTemplate)
	holoAPI.GET("/templates/:key/revisions", handler.GetTemplateRevisions)
	holoAPI.GET("/templates/:key/revisions/:id", handler.GetTemplateRevision)
}
