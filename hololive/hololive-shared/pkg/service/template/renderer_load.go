package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	templateLoadTimeout        = 5 * time.Second
	templateResolveMaxAttempts = 2
)

var (
	ErrTemplateNotFound      = errors.New("template not found in database")
	ErrInvalidTemplateFormat = errors.New("invalid template format")
	errTemplateSourceChanged = fmt.Errorf("template source changed: %w", ErrTemplateNotFound)
)

func (r *Renderer) getTemplate(ctx context.Context, key domain.TemplateKey, channelID string, format domain.TemplateFormat) (*template.Template, error) {
	if !format.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTemplateFormat, format)
	}

	ctx, cancel := context.WithTimeout(ctx, templateLoadTimeout)
	defer cancel()

	for attempt := 1; attempt <= templateResolveMaxAttempts; attempt++ {
		ck, err := r.resolveTemplate(ctx, key, channelID, format)
		if err != nil {
			return nil, err
		}

		tmpl, err := r.loadResolvedTemplate(ctx, ck)
		if err == nil {
			return tmpl, nil
		}

		if !errors.Is(err, errTemplateSourceChanged) {
			return nil, err
		}

		r.logSourceChange(ctx, key, attempt)
	}

	return nil, fmt.Errorf("template %s source changed after %d attempts: %w", key, templateResolveMaxAttempts, errTemplateSourceChanged)
}

func (r *Renderer) resolveTemplate(ctx context.Context, key domain.TemplateKey, channelID string, format domain.TemplateFormat) (cacheKey, error) {
	ck := cacheKey{templateKey: key}

	err := pgxdb.QueryRow(ctx, r.pool, rendererResolveSQL, key, channelID, format).Scan(&ck.id, &ck.version, &ck.channelID)
	if err != nil {
		return cacheKey{}, templateQueryError(key, err)
	}

	return ck, nil
}

func (r *Renderer) loadResolvedTemplate(ctx context.Context, ck cacheKey) (*template.Template, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load resolved template: %w", err)
	}

	if tmpl := r.cachedTemplate(ck); tmpl != nil {
		return tmpl, nil
	}

	resultCh := r.loads.DoChan(fmt.Sprintf("%d/%d", ck.id, ck.version), func() (any, error) {
		if tmpl := r.cachedTemplate(ck); tmpl != nil {
			return tmpl, nil
		}

		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), templateLoadTimeout)
		defer cancel()

		return r.loadTemplate(loadCtx, ck)
	})
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for template: %w", ctx.Err())
	case result := <-resultCh:
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for template: %w", err)
		}

		return templateLoadResult(result.Val, result.Err)
	}
}

func templateLoadResult(value any, err error) (*template.Template, error) {
	if err != nil {
		return nil, err
	}

	tmpl, ok := value.(*template.Template)
	if !ok || tmpl == nil {
		return nil, fmt.Errorf("load template: unexpected result %T", value)
	}

	return tmpl, nil
}

func (r *Renderer) logSourceChange(ctx context.Context, key domain.TemplateKey, attempt int) {
	if r.logger == nil {
		return
	}

	exhausted := attempt == templateResolveMaxAttempts
	level := slog.LevelInfo

	if exhausted {
		level = slog.LevelWarn
	}

	r.logger.LogAttrs(ctx, level, "template source changed",
		slog.String("template_key", string(key)), slog.Int("attempt", attempt),
		slog.Int("max_attempts", templateResolveMaxAttempts), slog.Bool("exhausted", exhausted))
}

func (r *Renderer) cachedTemplate(ck cacheKey) *template.Template {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	return r.cache[ck].tmpl
}

func (r *Renderer) loadTemplate(ctx context.Context, ck cacheKey) (*template.Template, error) {
	var body string

	err := pgxdb.QueryRow(ctx, r.pool, rendererLoadSQL, ck.id, ck.templateKey, ck.channelID).Scan(&body, &ck.version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", errTemplateSourceChanged, ck.templateKey)
	}

	if err != nil {
		return nil, templateQueryError(ck.templateKey, err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load template body: %w", err)
	}

	return r.parseTemplate(ck, body)
}

func (r *Renderer) parseTemplate(ck cacheKey, body string) (*template.Template, error) {
	value, err, _ := r.parses.Do(fmt.Sprintf("%d/%d", ck.id, ck.version), func() (any, error) {
		if tmpl := r.cachedTemplate(ck); tmpl != nil {
			return tmpl, nil
		}

		tmpl, err := parseTemplateBody(string(ck.templateKey), body, false)
		if err != nil {
			return nil, err
		}

		r.storeTemplateAt(ck, tmpl, time.Now())

		return tmpl, nil
	})

	return templateLoadResult(value, err)
}

func templateQueryError(key domain.TemplateKey, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrTemplateNotFound, key)
	}

	return fmt.Errorf("query template: %w", err)
}
