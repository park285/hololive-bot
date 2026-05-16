// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package api

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
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
	if !h.requireTemplateAdmin(c) {
		return
	}

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
		sharedserver.RespondError(c, 500, "failed to list templates", nil)
		return
	}

	c.JSON(200, templateListResponse{Templates: templates})
}

func (h *TemplateAPIHandler) GetTemplateByKey(c *gin.Context) {
	if !h.requireTemplateAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	defaultTmpl, overrides, err := h.templateAdmin.GetByKey(ctx, key)
	if err != nil {
		if errors.Is(err, template.ErrTemplateKeyNotFound) {
			sharedserver.RespondError(c, 404, "template not found", nil)
			return
		}

		h.safeLogger().Error("Failed to get template", slog.String("key", string(key)), slog.Any("error", err))
		sharedserver.RespondError(c, 500, "failed to get template", nil)

		return
	}

	c.JSON(200, templateDetailResponse{
		TemplateKey: key,
		Default:     defaultTmpl,
		Overrides:   overrides,
	})
}

func (h *TemplateAPIHandler) UpsertTemplate(c *gin.Context) {
	var req templateUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	if !h.requireTemplateAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	var channelPtr *string

	if ch := c.Query("channel_id"); ch != "" {
		channelPtr = &ch
	}

	tmpl, err := h.templateAdmin.Save(ctx, key, channelPtr, req.Body)
	if err != nil {
		h.respondTemplateSaveError(c, key, err)
		return
	}

	c.JSON(200, tmpl)
}

func (h *TemplateAPIHandler) respondTemplateSaveError(c *gin.Context, key domain.TemplateKey, err error) {
	h.safeLogger().Warn("Failed to save template", slog.String("key", string(key)), slog.Any("error", err))
	respondTemplateMutationError(c, err, "failed to save template")
}

func (h *TemplateAPIHandler) DeleteTemplateOverride(c *gin.Context) {
	if !h.requireTemplateAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))
	channelID := c.Query("channel_id")

	err := h.templateAdmin.DeleteOverride(ctx, key, channelID)
	if err != nil {
		if errors.Is(err, template.ErrChannelIDRequired) {
			sharedserver.RespondError(c, 400, "channel_id required for delete (cannot delete default template)", nil)
			return
		}

		sharedserver.RespondError(c, 500, "failed to delete override", nil)

		return
	}

	c.JSON(200, gin.H{"message": "override deleted; default template is now active"})
}

func (h *TemplateAPIHandler) PreviewTemplate(c *gin.Context) {
	var req templatePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.safeLogger().Warn("Invalid request body", slog.Any("error", err))
		sharedserver.RespondError(c, 400, "invalid request body", nil)

		return
	}

	if !h.requireTemplateAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	rendered, sampleData, err := h.templateAdmin.Preview(ctx, key, req.Body)
	if err != nil {
		h.respondTemplatePreviewError(c, key, err)
		return
	}

	c.JSON(200, templatePreviewResponse{
		Rendered:       rendered,
		SampleDataUsed: sampleData,
	})
}

func (h *TemplateAPIHandler) respondTemplatePreviewError(c *gin.Context, key domain.TemplateKey, err error) {
	h.safeLogger().Warn("Failed to preview template", slog.String("key", string(key)), slog.Any("error", err))
	respondTemplateMutationError(c, err, "failed to preview template")
}

func respondTemplateMutationError(c *gin.Context, err error, defaultMessage string) {
	switch {
	case errors.Is(err, template.ErrTemplateKeyNotFound):
		sharedserver.RespondError(c, 404, "template not found", nil)
	case errors.Is(err, template.ErrTemplateParseError):
		sharedserver.RespondError(c, 400, "template parse error", nil)
	case errors.Is(err, template.ErrTemplateRenderError):
		sharedserver.RespondError(c, 400, "template render error", nil)
	default:
		sharedserver.RespondError(c, 500, defaultMessage, nil)
	}
}

func (h *TemplateAPIHandler) GetTemplateRevisions(c *gin.Context) {
	if !h.requireTemplateAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	key := domain.TemplateKey(c.Param("key"))

	var channelPtr *string

	if ch := c.Query("channel_id"); ch != "" {
		channelPtr = &ch
	}

	revisions, err := h.templateAdmin.GetRevisions(ctx, key, channelPtr)
	if err != nil {
		sharedserver.RespondError(c, 500, "failed to get revisions", nil)
		return
	}

	c.JSON(200, templateRevisionsResponse{
		TemplateKey: key,
		ChannelID:   channelPtr,
		Revisions:   revisions,
	})
}

func (h *TemplateAPIHandler) GetTemplateRevision(c *gin.Context) {
	if !h.requireTemplateAdmin(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	idStr := c.Param("id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		sharedserver.RespondError(c, 400, "invalid revision id", nil)
		return
	}

	revision, err := h.templateAdmin.GetRevisionByID(ctx, id)
	if err != nil {
		if errors.Is(err, template.ErrRevisionNotFound) {
			sharedserver.RespondError(c, 404, "revision not found", nil)
			return
		}

		h.safeLogger().Error("Failed to get revision", slog.Int64("id", id), slog.Any("error", err))
		sharedserver.RespondError(c, 500, "failed to get revision", nil)

		return
	}

	c.JSON(200, revision)
}
