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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"text/template"

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
	if !domain.IsValidTemplateKey(key) {
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
	if !domain.IsValidTemplateKey(key) {
		return nil, fmt.Errorf("%w: %s", ErrTemplateKeyNotFound, key)
	}

	if err := s.validateTemplate(key, body); err != nil {
		return nil, err
	}

	existing, err := s.repo.FindByKeyAndChannel(ctx, key, channelID)
	if err != nil {
		return nil, fmt.Errorf("find template: %w", err)
	}

	if existing != nil && existing.Body != body {
		if revErr := s.repo.CreateRevision(ctx, existing.ID, existing.Body); revErr != nil {
			s.logger.Warn("failed to create revision", slog.Any("error", revErr))
		}
		if pruneErr := s.repo.PruneOldRevisions(ctx, existing.ID, maxRevisions); pruneErr != nil {
			s.logger.Warn("failed to prune revisions", slog.Any("error", pruneErr))
		}
	}

	result, err := s.repo.Upsert(ctx, key, channelID, body)
	if err != nil {
		return nil, fmt.Errorf("upsert template: %w", err)
	}

	if channelID != nil {
		s.renderer.InvalidateCache(key, *channelID)
	} else {
		s.renderer.InvalidateKey(key)
	}

	return result, nil
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

func (s *AdminService) Preview(ctx context.Context, key domain.TemplateKey, body string) (string, any, error) {
	if !domain.IsValidTemplateKey(key) {
		return "", nil, fmt.Errorf("%w: %s", ErrTemplateKeyNotFound, key)
	}

	sampleData := domain.GetTemplateSampleData(key)
	if sampleData == nil {
		return "", nil, fmt.Errorf("%w: no sample data for %s", ErrTemplateKeyNotFound, key)
	}

	tmpl, err := template.New(string(key)).Funcs(templateFuncs).Option("missingkey=error").Parse(body)
	if err != nil {
		return "", nil, errors.Join(ErrTemplateParseError, fmt.Errorf("parse failed: %w", err))
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, sampleData); err != nil {
		return "", nil, errors.Join(ErrTemplateRenderError, fmt.Errorf("render failed: %w", err))
	}

	return buf.String(), sampleData, nil
}

func (s *AdminService) GetRevisions(ctx context.Context, key domain.TemplateKey, channelID *string) ([]*domain.NotificationTemplateRevision, error) {
	tmpl, err := s.repo.FindByKeyAndChannel(ctx, key, channelID)
	if err != nil {
		return nil, fmt.Errorf("find template: %w", err)
	}
	if tmpl == nil {
		return nil, nil
	}

	revisions, err := s.repo.GetRevisions(ctx, tmpl.ID, maxRevisions)
	if err != nil {
		return nil, fmt.Errorf("get revisions: %w", err)
	}
	return revisions, nil
}

func (s *AdminService) GetRevisionByID(ctx context.Context, id int64) (*domain.NotificationTemplateRevision, error) {
	rev, err := s.repo.GetRevisionByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get revision %d: %w", id, err)
	}
	if rev == nil {
		return nil, ErrRevisionNotFound
	}
	return rev, nil
}

func (s *AdminService) validateTemplate(key domain.TemplateKey, body string) error {
	tmpl, err := template.New(string(key)).Funcs(templateFuncs).Option("missingkey=error").Parse(body)
	if err != nil {
		return errors.Join(ErrTemplateParseError, fmt.Errorf("parse failed: %w", err))
	}

	sampleData := domain.GetTemplateSampleData(key)
	if sampleData == nil {
		return nil
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, sampleData); err != nil {
		return errors.Join(ErrTemplateRenderError, fmt.Errorf("render failed: %w", err))
	}

	return nil
}
