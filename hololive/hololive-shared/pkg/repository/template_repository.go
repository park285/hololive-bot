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
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	query, args := templateListQuery(key, channelID)

	var templates []*domain.NotificationTemplate
	if err := pgxscan.Select(ctx, r.pool, &templates, query, args...); err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	return templates, nil
}

func templateListQuery(key *domain.TemplateKey, channelID *string) (query string, args []any) {
	switch {
	case key != nil && channelID != nil:
		return templateListByKeyAndChannelSQL, []any{*key, *channelID}
	case key != nil:
		return templateListByKeySQL, []any{*key}
	case channelID != nil:
		return templateListByChannelSQL, []any{*channelID}
	default:
		return templateListSQL, nil
	}
}

func (r *TemplateRepository) FindByKeyAndChannel(ctx context.Context, key domain.TemplateKey, channelID *string) (*domain.NotificationTemplate, error) {
	query := templateFindDefaultSQL
	args := []any{key}
	if channelID != nil {
		query = templateFindOverrideSQL
		args = append(args, *channelID)
	}

	var tmpl domain.NotificationTemplate
	err := pgxscan.Get(ctx, r.pool, &tmpl, query, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find template: %w", err)
	}

	return &tmpl, nil
}

// Upsert는 default(ux_notification_templates_default)와
// override(ux_notification_templates_channel) 부분 유니크 인덱스를 각각 arbiter로
// 쓴다. 하나의 INSERT는 인덱스 하나만 추론할 수 있어 변형이 둘로 나뉜다.
func (r *TemplateRepository) Upsert(ctx context.Context, key domain.TemplateKey, channelID *string, body string) (*domain.NotificationTemplate, error) {
	tmpl, _, err := r.UpsertWithPreviousBody(ctx, key, channelID, body)
	return tmpl, err
}

// UpsertWithPreviousBody는 PostgreSQL 18 RETURNING OLD/NEW로 실제 덮어쓴 본문을
// 같은 UPSERT 결과에서 돌려준다. 호출자는 별도 선행 SELECT 없이 정확한 revision을
// 만들 수 있다.
func (r *TemplateRepository) UpsertWithPreviousBody(
	ctx context.Context,
	key domain.TemplateKey,
	channelID *string,
	body string,
) (*domain.NotificationTemplate, *string, error) {
	return upsertTemplateRow(ctx, r.pool, key, channelID, body)
}

// UpsertWithRevision은 본문 교체와 직전 본문 revision 기록, 보존 개수 정리를 한
// 트랜잭션에 묶는다. UPSERT가 잡은 템플릿 행 잠금이 커밋까지 유지되므로 동시 저장이
// 서로의 revision 사이에 끼어들지 못한다.
func (r *TemplateRepository) UpsertWithRevision(
	ctx context.Context,
	key domain.TemplateKey,
	channelID *string,
	body string,
	keepRevisions int,
) (*domain.NotificationTemplate, *string, error) {
	var (
		tmpl         *domain.NotificationTemplate
		previousBody *string
	)

	if err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		tmpl, previousBody, err = upsertTemplateRow(ctx, tx, key, channelID, body)
		if err != nil {
			return err
		}
		if previousBody == nil || *previousBody == body {
			return nil
		}
		return recordRevision(ctx, tx, tmpl.ID, *previousBody, keepRevisions)
	}); err != nil {
		return nil, nil, fmt.Errorf("upsert template with revision: %w", err)
	}

	if tmpl == nil {
		return nil, nil, fmt.Errorf("upsert template with revision: no row returned")
	}

	return tmpl, previousBody, nil
}

// 트랜잭션 안에서는 created_at 기본값 now()가 BEGIN 시각으로 고정된다. 먼저 BEGIN한
// 트랜잭션이 템플릿 행 잠금을 나중에 얻으면 더 이른 시각이 기록되어 revision_list와
// revision_prune의 ORDER BY created_at DESC가 뒤집힌다. clock_timestamp()는 잠금을
// 얻은 뒤의 실제 실행 시각을 남겨 순서를 잠금 획득 순서와 일치시킨다.
func recordRevision(ctx context.Context, tx pgx.Tx, templateID int64, body string, keepRevisions int) error {
	if _, err := tx.Exec(ctx, revisionInsertAtClockSQL, templateID, body); err != nil {
		return fmt.Errorf("create revision: %w", err)
	}
	if keepRevisions <= 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, revisionPruneSQL, templateID, keepRevisions); err != nil {
		return fmt.Errorf("prune revisions: %w", err)
	}
	return nil
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func upsertTemplateRow(
	ctx context.Context,
	q rowQuerier,
	key domain.TemplateKey,
	channelID *string,
	body string,
) (*domain.NotificationTemplate, *string, error) {
	query := templateUpsertDefaultSQL
	args := []any{key, body}
	if channelID != nil {
		query = templateUpsertOverrideSQL
		args = []any{key, *channelID, body}
	}

	var (
		tmpl             domain.NotificationTemplate
		templateKey      string
		returnedChannel  sql.NullString
		previousBodyText sql.NullString
	)
	if err := q.QueryRow(ctx, query, args...).Scan(
		&tmpl.ID,
		&templateKey,
		&returnedChannel,
		&tmpl.Body,
		&tmpl.CreatedAt,
		&tmpl.UpdatedAt,
		&previousBodyText,
	); err != nil {
		return nil, nil, fmt.Errorf("upsert template: %w", err)
	}
	tmpl.TemplateKey = domain.TemplateKey(templateKey)
	if returnedChannel.Valid {
		value := returnedChannel.String
		tmpl.ChannelID = &value
	}

	var previousBody *string
	if previousBodyText.Valid {
		value := previousBodyText.String
		previousBody = &value
	}

	return &tmpl, previousBody, nil
}

func (r *TemplateRepository) DeleteOverride(ctx context.Context, key domain.TemplateKey, channelID string) error {
	if _, err := r.pool.Exec(ctx,
		templateDeleteOverrideSQL,
		key,
		channelID,
	); err != nil {
		return fmt.Errorf("delete override: %w", err)
	}
	return nil
}

func (r *TemplateRepository) GetByKey(ctx context.Context, key domain.TemplateKey) (defaultTmpl *domain.NotificationTemplate, overrides []*domain.NotificationTemplate, err error) {
	var tmpl domain.NotificationTemplate
	err = pgxscan.Get(ctx, r.pool, &tmpl,
		templateFindDefaultSQL,
		key,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("get default template: %w", err)
	}
	if err == nil {
		defaultTmpl = &tmpl
	}

	if err := pgxscan.Select(ctx, r.pool, &overrides,
		templateListOverridesSQL,
		key,
	); err != nil {
		return nil, nil, fmt.Errorf("get overrides: %w", err)
	}

	return defaultTmpl, overrides, nil
}

func (r *TemplateRepository) CreateRevision(ctx context.Context, templateID int64, body string) error {
	if _, err := r.pool.Exec(ctx,
		revisionInsertSQL,
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
		revisionListSQL,
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
		revisionGetSQL,
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

	if _, err := r.pool.Exec(ctx,
		revisionPruneSQL,
		templateID,
		keepCount,
	); err != nil {
		return fmt.Errorf("prune revisions: %w", err)
	}
	return nil
}
