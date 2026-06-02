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

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type TemplateRepository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewTemplateRepository(pool *pgxpool.Pool, logger *slog.Logger) *TemplateRepository {
	return &TemplateRepository{
		pool:   pool,
		logger: logger,
	}
}

func (r *TemplateRepository) List(ctx context.Context, key *domain.TemplateKey, channelID *string) ([]*domain.NotificationTemplate, error) {
	query := `SELECT id, template_key, channel_id, body, created_at, updated_at FROM notification_templates`
	args := make([]any, 0, 2)
	conditions := make([]string, 0, 2)

	if key != nil {
		args = append(args, *key)
		conditions = append(conditions, fmt.Sprintf("template_key = $%d", len(args)))
	}
	if channelID != nil {
		args = append(args, *channelID)
		conditions = append(conditions, fmt.Sprintf("channel_id = $%d", len(args)))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY template_key, channel_id"

	var templates []*domain.NotificationTemplate
	if err := pgxscan.Select(ctx, r.pool, &templates, query, args...); err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	return templates, nil
}

func (r *TemplateRepository) FindByKeyAndChannel(ctx context.Context, key domain.TemplateKey, channelID *string) (*domain.NotificationTemplate, error) {
	var tmpl domain.NotificationTemplate
	query := `SELECT id, template_key, channel_id, body, created_at, updated_at
		FROM notification_templates
		WHERE template_key = $1`
	args := []any{key}
	if channelID == nil {
		query += " AND channel_id IS NULL"
	} else {
		args = append(args, *channelID)
		query += " AND channel_id = $2"
	}

	err := pgxscan.Get(ctx, r.pool, &tmpl, query, args...)
	if errors.Is(err, pgx.ErrNoRows) {
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
	var newTmpl domain.NotificationTemplate
	err := pgxscan.Get(ctx, r.pool, &newTmpl,
		`INSERT INTO notification_templates(template_key, channel_id, body)
		 VALUES ($1, $2, $3)
		 RETURNING id, template_key, channel_id, body, created_at, updated_at`,
		key,
		channelID,
		body,
	)
	if err != nil {
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
	err := pgxscan.Get(ctx, r.pool, existing,
		`UPDATE notification_templates
		 SET body = $1, updated_at = NOW()
		 WHERE id = $2
		 RETURNING id, template_key, channel_id, body, created_at, updated_at`,
		body,
		existing.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return existing, nil
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
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
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM notification_templates WHERE template_key = $1 AND channel_id = $2`,
		key,
		channelID,
	); err != nil {
		return fmt.Errorf("delete override: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetByKey(ctx context.Context, key domain.TemplateKey) (*domain.NotificationTemplate, []*domain.NotificationTemplate, error) {
	var defaultTmpl *domain.NotificationTemplate
	var overrides []*domain.NotificationTemplate

	var tmpl domain.NotificationTemplate
	err := pgxscan.Get(ctx, r.pool, &tmpl,
		`SELECT id, template_key, channel_id, body, created_at, updated_at
		 FROM notification_templates
		 WHERE template_key = $1 AND channel_id IS NULL`,
		key,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("get default template: %w", err)
	}
	if err == nil {
		defaultTmpl = &tmpl
	}

	if err := pgxscan.Select(ctx, r.pool, &overrides,
		`SELECT id, template_key, channel_id, body, created_at, updated_at
		 FROM notification_templates
		 WHERE template_key = $1 AND channel_id IS NOT NULL
		 ORDER BY channel_id`,
		key,
	); err != nil {
		return nil, nil, fmt.Errorf("get overrides: %w", err)
	}

	return defaultTmpl, overrides, nil
}

func (r *TemplateRepository) CreateRevision(ctx context.Context, templateID int64, body string) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO notification_template_revisions(template_id, body) VALUES ($1, $2)`,
		templateID,
		body,
	); err != nil {
		return fmt.Errorf("create revision: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetRevisions(ctx context.Context, templateID int64, limit int) ([]*domain.NotificationTemplateRevision, error) {
	var revisions []*domain.NotificationTemplateRevision
	if err := pgxscan.Select(ctx, r.pool, &revisions,
		`SELECT id, template_id, body, created_at
		 FROM notification_template_revisions
		 WHERE template_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		templateID,
		limit,
	); err != nil {
		return nil, fmt.Errorf("get revisions: %w", err)
	}
	return revisions, nil
}

func (r *TemplateRepository) GetRevisionByID(ctx context.Context, id int64) (*domain.NotificationTemplateRevision, error) {
	var revision domain.NotificationTemplateRevision
	err := pgxscan.Get(ctx, r.pool, &revision,
		`SELECT id, template_id, body, created_at
		 FROM notification_template_revisions
		 WHERE id = $1`,
		id,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get revision: %w", err)
	}
	return &revision, nil
}

func (r *TemplateRepository) PruneOldRevisions(ctx context.Context, templateID int64, keepCount int) error {
	if keepCount <= 0 {
		return nil
	}

	return dbx.InPgxTx(ctx, r.pool, func(tx dbx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM notification_template_revisions
			 WHERE template_id = $1
			   AND id NOT IN (
			       SELECT id
			       FROM notification_template_revisions
			       WHERE template_id = $1
			       ORDER BY created_at DESC
			       LIMIT $2
			   )`,
			templateID,
			keepCount,
		); err != nil {
			return fmt.Errorf("prune revisions: %w", err)
		}
		return nil
	})
}
