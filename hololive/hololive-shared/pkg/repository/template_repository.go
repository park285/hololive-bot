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

package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type TemplateRepository struct {
	db     *gorm.DB
	logger *slog.Logger
}

func NewTemplateRepository(db *gorm.DB, logger *slog.Logger) *TemplateRepository {
	return &TemplateRepository{
		db:     db,
		logger: logger,
	}
}

func (r *TemplateRepository) List(ctx context.Context, key *domain.TemplateKey, channelID *string) ([]*domain.NotificationTemplate, error) {
	query := r.db.WithContext(ctx).Model(&domain.NotificationTemplate{})

	if key != nil {
		query = query.Where("template_key = ?", *key)
	}
	if channelID != nil {
		query = query.Where("channel_id = ?", *channelID)
	}

	var templates []*domain.NotificationTemplate
	if err := query.Order("template_key, channel_id").Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	return templates, nil
}

func (r *TemplateRepository) FindByKeyAndChannel(ctx context.Context, key domain.TemplateKey, channelID *string) (*domain.NotificationTemplate, error) {
	var tmpl domain.NotificationTemplate
	query := r.db.WithContext(ctx).Where("template_key = ?", key)

	if channelID == nil {
		query = query.Where("channel_id IS NULL")
	} else {
		query = query.Where("channel_id = ?", *channelID)
	}

	err := query.First(&tmpl).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find template: %w", err)
	}

	return &tmpl, nil
}

func (r *TemplateRepository) Upsert(ctx context.Context, key domain.TemplateKey, channelID *string, body string) (*domain.NotificationTemplate, error) {
	existing, err := r.FindByKeyAndChannel(ctx, key, channelID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		return r.createTemplate(ctx, key, channelID, body)
	}

	return r.updateTemplate(ctx, existing, body)
}

func (r *TemplateRepository) createTemplate(ctx context.Context, key domain.TemplateKey, channelID *string, body string) (*domain.NotificationTemplate, error) {
	newTmpl := domain.NotificationTemplate{
		TemplateKey: key,
		ChannelID:   channelID,
		Body:        body,
	}
	if err := r.db.WithContext(ctx).Create(&newTmpl).Error; err != nil {
		return r.handleCreateTemplateError(ctx, key, channelID, body, err)
	}
	return &newTmpl, nil
}

func (r *TemplateRepository) handleCreateTemplateError(ctx context.Context, key domain.TemplateKey, channelID *string, body string, err error) (*domain.NotificationTemplate, error) {
	if !isDuplicateKeyError(err) {
		return nil, fmt.Errorf("create template: %w", err)
	}

	retryTarget, findErr := r.FindByKeyAndChannel(ctx, key, channelID)
	if findErr != nil {
		return nil, fmt.Errorf("find template after duplicate key: %w", findErr)
	}
	if retryTarget == nil {
		return nil, fmt.Errorf("create template: %w", err)
	}

	if _, saveErr := r.updateTemplate(ctx, retryTarget, body); saveErr != nil {
		return nil, fmt.Errorf("update template after duplicate key: %w", saveErr)
	}
	return retryTarget, nil
}

func (r *TemplateRepository) updateTemplate(ctx context.Context, existing *domain.NotificationTemplate, body string) (*domain.NotificationTemplate, error) {
	existing.Body = body
	if err := r.db.WithContext(ctx).Save(existing).Error; err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return existing, nil
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	if strings.Contains(msg, "duplicate key value violates unique constraint") {
		return true
	}

	return false
}

func (r *TemplateRepository) DeleteOverride(ctx context.Context, key domain.TemplateKey, channelID string) error {
	result := r.db.WithContext(ctx).
		Where("template_key = ? AND channel_id = ?", key, channelID).
		Delete(&domain.NotificationTemplate{})

	if result.Error != nil {
		return fmt.Errorf("delete override: %w", result.Error)
	}
	return nil
}

func (r *TemplateRepository) GetByKey(ctx context.Context, key domain.TemplateKey) (*domain.NotificationTemplate, []*domain.NotificationTemplate, error) {
	var defaultTmpl *domain.NotificationTemplate
	var overrides []*domain.NotificationTemplate

	var tmpl domain.NotificationTemplate
	err := r.db.WithContext(ctx).
		Where("template_key = ? AND channel_id IS NULL", key).
		First(&tmpl).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, fmt.Errorf("get default template: %w", err)
	}
	if err == nil {
		defaultTmpl = &tmpl
	}

	err = r.db.WithContext(ctx).
		Where("template_key = ? AND channel_id IS NOT NULL", key).
		Order("channel_id").
		Find(&overrides).Error
	if err != nil {
		return nil, nil, fmt.Errorf("get overrides: %w", err)
	}

	return defaultTmpl, overrides, nil
}

func (r *TemplateRepository) CreateRevision(ctx context.Context, templateID int64, body string) error {
	revision := domain.NotificationTemplateRevision{
		TemplateID: templateID,
		Body:       body,
	}
	if err := r.db.WithContext(ctx).Create(&revision).Error; err != nil {
		return fmt.Errorf("create revision: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetRevisions(ctx context.Context, templateID int64, limit int) ([]*domain.NotificationTemplateRevision, error) {
	var revisions []*domain.NotificationTemplateRevision
	err := r.db.WithContext(ctx).
		Where("template_id = ?", templateID).
		Order("created_at DESC").
		Limit(limit).
		Find(&revisions).Error
	if err != nil {
		return nil, fmt.Errorf("get revisions: %w", err)
	}
	return revisions, nil
}

func (r *TemplateRepository) GetRevisionByID(ctx context.Context, id int64) (*domain.NotificationTemplateRevision, error) {
	var revision domain.NotificationTemplateRevision
	err := r.db.WithContext(ctx).First(&revision, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get revision: %w", err)
	}
	return &revision, nil
}

func (r *TemplateRepository) PruneOldRevisions(ctx context.Context, templateID int64, keepCount int) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.NotificationTemplateRevision{}).
		Where("template_id = ?", templateID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("count revisions: %w", err)
	}

	if count <= int64(keepCount) {
		return nil
	}

	var toKeep []int64
	err := r.db.WithContext(ctx).Model(&domain.NotificationTemplateRevision{}).
		Where("template_id = ?", templateID).
		Order("created_at DESC").
		Limit(keepCount).
		Pluck("id", &toKeep).Error
	if err != nil {
		return fmt.Errorf("get revisions to keep: %w", err)
	}

	if len(toKeep) == 0 {
		return nil
	}

	err = r.db.WithContext(ctx).
		Where("template_id = ? AND id NOT IN ?", templateID, toKeep).
		Delete(&domain.NotificationTemplateRevision{}).Error
	if err != nil {
		return fmt.Errorf("prune revisions: %w", err)
	}

	return nil
}
