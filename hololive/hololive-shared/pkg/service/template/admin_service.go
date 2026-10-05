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

package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
)

var (
	ErrTemplateKeyNotFound = errors.New("template key not found")
	ErrTemplateParseError  = errors.New("template parse error")
	ErrTemplateRenderError = errors.New("template render error")
	ErrRevisionNotFound    = errors.New("revision not found")
	ErrChannelIDRequired   = errors.New("channel_id required for delete")
)

const maxRevisions = 5

type AdminService struct {
	repo     *repository.TemplateRepository
	renderer *Renderer
	logger   *slog.Logger
}

func NewAdminService(repo *repository.TemplateRepository, renderer *Renderer, logger *slog.Logger) *AdminService {
	return &AdminService{
		repo:     repo,
		renderer: renderer,
		logger:   logger,
	}
}

func (s *AdminService) List(ctx context.Context, key *domain.TemplateKey, channelID *string) ([]*domain.NotificationTemplate, error) {
	templates, err := s.repo.List(ctx, key, channelID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	return templates, nil
}

func (s *AdminService) GetByKey(ctx context.Context, key domain.TemplateKey) (*domain.NotificationTemplate, []*domain.NotificationTemplate, error) {
	if !sampledata.IsValidTemplateKey(key) {
		return nil, nil, fmt.Errorf("%w: %s", ErrTemplateKeyNotFound, key)
	}

	defaultTmpl, overrides, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return nil, nil, fmt.Errorf("get template by key %s: %w", key, err)
	}

	if defaultTmpl == nil && len(overrides) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrTemplateKeyNotFound, key)
	}

	return defaultTmpl, overrides, nil
}

func (s *AdminService) Save(ctx context.Context, key domain.TemplateKey, channelID *string, body string) (*domain.NotificationTemplate, error) {
	if !sampledata.IsValidTemplateKey(key) {
		return nil, fmt.Errorf("%w: %s", ErrTemplateKeyNotFound, key)
	}

	if err := s.validateTemplate(ctx, key, body); err != nil {
		return nil, fmt.Errorf("validate template: %w", err)
	}

	result, _, err := s.repo.UpsertWithRevision(ctx, key, channelID, body, maxRevisions)
	if err != nil {
		return nil, fmt.Errorf("upsert template: %w", err)
	}

	s.invalidateRendererCache(key, channelID)

	return result, nil
}

func (s *AdminService) invalidateRendererCache(key domain.TemplateKey, channelID *string) {
	if channelID != nil {
		s.renderer.InvalidateCache(key, *channelID)

		return
	}

	s.renderer.InvalidateKey(key)
}

func (s *AdminService) DeleteOverride(ctx context.Context, key domain.TemplateKey, channelID string) error {
	if channelID == "" {
		return ErrChannelIDRequired
	}

	if err := s.repo.DeleteOverride(ctx, key, channelID); err != nil {
		return fmt.Errorf("delete template override: %w", err)
	}

	s.renderer.InvalidateCache(key, channelID)

	return nil
}

// Preview는 저장하지 않은 본문을 표본 데이터로 렌더링합니다. 실행은 Render와 같은 취소·출력·단계·시간 예산을 따르며
// 실패하면 부분 결과 없이 ErrTemplateRenderError를 돌려줍니다.
func (s *AdminService) Preview(ctx context.Context, key domain.TemplateKey, body string) (string, any, error) {
	if !sampledata.IsValidTemplateKey(key) {
		return "", nil, fmt.Errorf("%w: %s", ErrTemplateKeyNotFound, key)
	}

	sampleData := sampledata.GetTemplateSampleData(key)
	if sampleData == nil {
		return "", nil, fmt.Errorf("%w: no sample data for %s", ErrTemplateKeyNotFound, key)
	}

	out, err := renderTemplateBody(ctx, key, body, sampleData)
	if err != nil {
		return "", nil, err
	}

	return out, sampleData, nil
}

func (s *AdminService) GetRevisions(ctx context.Context, key domain.TemplateKey, channelID *string) ([]*domain.NotificationTemplateRevision, error) {
	tmpl, found, err := s.repo.FindByKeyAndChannel(ctx, key, channelID)
	if err != nil {
		return nil, fmt.Errorf("find template: %w", err)
	}

	if !found {
		return nil, nil
	}

	revisions, err := s.repo.GetRevisions(ctx, tmpl.ID, maxRevisions)
	if err != nil {
		return nil, fmt.Errorf("get revisions: %w", err)
	}

	return revisions, nil
}

func (s *AdminService) GetRevisionByID(ctx context.Context, id int64) (*domain.NotificationTemplateRevision, error) {
	rev, found, err := s.repo.GetRevisionByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get revision %d: %w", id, err)
	}

	if !found {
		return nil, ErrRevisionNotFound
	}

	return &rev, nil
}

// validateTemplate는 저장 전 본문을 파싱하고, 표본 데이터가 있으면 Preview와 같은 예산으로 실행해 봅니다.
func (s *AdminService) validateTemplate(ctx context.Context, key domain.TemplateKey, body string) error {
	sampleData := sampledata.GetTemplateSampleData(key)
	if sampleData == nil {
		if _, err := parseTemplateBody(string(key), body, true); err != nil {
			return errors.Join(ErrTemplateParseError, fmt.Errorf("parse failed: %w", err))
		}

		return nil
	}

	_, err := renderTemplateBody(ctx, key, body, sampleData)

	return err
}

func renderTemplateBody(ctx context.Context, key domain.TemplateKey, body string, data any) (string, error) {
	tmpl, err := parseTemplateBody(string(key), body, true)
	if err != nil {
		return "", errors.Join(ErrTemplateParseError, fmt.Errorf("parse failed: %w", err))
	}

	out, err := executeTemplate(ctx, tmpl, data)
	if err != nil {
		return "", errors.Join(ErrTemplateRenderError, fmt.Errorf("render failed: %w", err))
	}

	return out, nil
}
