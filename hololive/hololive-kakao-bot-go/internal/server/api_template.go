package server

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type templateListResponse struct {
	Templates []*domain.NotificationTemplate `json:"templates"`
}

type templateDetailResponse struct {
	TemplateKey domain.TemplateKey             `json:"template_key"`
	Default     *domain.NotificationTemplate   `json:"default,omitempty"`
	Overrides   []*domain.NotificationTemplate `json:"overrides"`
}

type templateUpsertRequest struct {
	Body string `json:"body" binding:"required"`
}

type templatePreviewRequest struct {
	Body string `json:"body" binding:"required"`
}

type templatePreviewResponse struct {
	Rendered       string `json:"rendered"`
	SampleDataUsed any    `json:"sample_data_used"`
}

type templateRevisionsResponse struct {
	TemplateKey domain.TemplateKey                     `json:"template_key"`
	ChannelID   *string                                `json:"channel_id,omitempty"`
	Revisions   []*domain.NotificationTemplateRevision `json:"revisions"`
}

func (h *TemplateAPIHandler) GetTemplates(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	var keyPtr *domain.TemplateKey
	if keyStr := c.Query("template_key"); keyStr != "" {
		key := domain.TemplateKey(keyStr)
		keyPtr = &key
	}

	var channelPtr *string
	if ch := c.Query("channel_id"); ch != "" {
		channelPtr = &ch
	}

	templates, err := h.templateAdmin.List(ctx, keyPtr, channelPtr)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list templates"})
		return
	}

	c.JSON(200, templateListResponse{Templates: templates})
}

func (h *TemplateAPIHandler) GetTemplateByKey(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	defaultTmpl, overrides, err := h.templateAdmin.GetByKey(ctx, key)
	if err != nil {
		if errors.Is(err, template.ErrTemplateKeyNotFound) {
			c.JSON(404, gin.H{"error": "template not found"})
			return
		}
		h.logger.Error("Failed to get template", slog.String("key", string(key)), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "failed to get template"})
		return
	}

	c.JSON(200, templateDetailResponse{
		TemplateKey: key,
		Default:     defaultTmpl,
		Overrides:   overrides,
	})
}

func (h *TemplateAPIHandler) UpsertTemplate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	var req templateUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	var channelPtr *string
	if ch := c.Query("channel_id"); ch != "" {
		channelPtr = &ch
	}

	tmpl, err := h.templateAdmin.Save(ctx, key, channelPtr, req.Body)
	if err != nil {
		h.logger.Warn("Failed to save template", slog.String("key", string(key)), slog.Any("error", err))
		switch {
		case errors.Is(err, template.ErrTemplateKeyNotFound):
			c.JSON(404, gin.H{"error": "template not found"})
		case errors.Is(err, template.ErrTemplateParseError):
			c.JSON(400, gin.H{"error": "template parse error"})
		case errors.Is(err, template.ErrTemplateRenderError):
			c.JSON(400, gin.H{"error": "template render error"})
		default:
			c.JSON(500, gin.H{"error": "failed to save template"})
		}
		return
	}

	c.JSON(200, tmpl)
}

func (h *TemplateAPIHandler) DeleteTemplateOverride(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))
	channelID := c.Query("channel_id")

	err := h.templateAdmin.DeleteOverride(ctx, key, channelID)
	if err != nil {
		if errors.Is(err, template.ErrChannelIDRequired) {
			c.JSON(400, gin.H{"error": "channel_id required for delete (cannot delete default template)"})
			return
		}
		c.JSON(500, gin.H{"error": "failed to delete override"})
		return
	}

	c.JSON(200, gin.H{"message": "override deleted; default template is now active"})
}

func (h *TemplateAPIHandler) PreviewTemplate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	var req templatePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Warn("Invalid request body", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	rendered, sampleData, err := h.templateAdmin.Preview(ctx, key, req.Body)
	if err != nil {
		h.logger.Warn("Failed to preview template", slog.String("key", string(key)), slog.Any("error", err))
		switch {
		case errors.Is(err, template.ErrTemplateKeyNotFound):
			c.JSON(404, gin.H{"error": "template not found"})
		case errors.Is(err, template.ErrTemplateParseError):
			c.JSON(400, gin.H{"error": "template parse error"})
		case errors.Is(err, template.ErrTemplateRenderError):
			c.JSON(400, gin.H{"error": "template render error"})
		default:
			c.JSON(500, gin.H{"error": "failed to preview template"})
		}
		return
	}

	c.JSON(200, templatePreviewResponse{
		Rendered:       rendered,
		SampleDataUsed: sampleData,
	})
}

func (h *TemplateAPIHandler) GetTemplateRevisions(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	var channelPtr *string
	if ch := c.Query("channel_id"); ch != "" {
		channelPtr = &ch
	}

	revisions, err := h.templateAdmin.GetRevisions(ctx, key, channelPtr)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get revisions"})
		return
	}

	c.JSON(200, templateRevisionsResponse{
		TemplateKey: key,
		ChannelID:   channelPtr,
		Revisions:   revisions,
	})
}

func (h *TemplateAPIHandler) GetTemplateRevision(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid revision id"})
		return
	}

	revision, err := h.templateAdmin.GetRevisionByID(ctx, id)
	if err != nil {
		if errors.Is(err, template.ErrRevisionNotFound) {
			c.JSON(404, gin.H{"error": "revision not found"})
			return
		}
		h.logger.Error("Failed to get revision", slog.Int64("id", id), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "failed to get revision"})
		return
	}

	c.JSON(200, revision)
}
