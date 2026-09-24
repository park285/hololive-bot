package template

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	templateLoadTimeout        = 5 * time.Second
	templateResolveMaxAttempts = 2
)

var (
	ErrTemplateNotFound      = errors.New("template not found in database")
	errTemplateSourceChanged = fmt.Errorf("template source changed: %w", ErrTemplateNotFound)
)

func (r *Renderer) getTemplate(ctx context.Context, key domain.TemplateKey, channelID string) (*template.Template, error) {
	// 최초 pool 대기부터 제한하며 소스 재확인으로 대기 예산을 연장하지 않습니다.
	ctx, cancel := context.WithTimeout(ctx, templateLoadTimeout)
	defer cancel()

	for attempt := 1; attempt <= templateResolveMaxAttempts; attempt++ {
		ck, err := r.resolveTemplate(ctx, key, channelID)
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

func (r *Renderer) resolveTemplate(ctx context.Context, key domain.TemplateKey, channelID string) (cacheKey, error) {
	// 저장 완료 뒤 시작한 호출이 이전 버전의 진행 중 조회에 합류하지 않도록 먼저 확인합니다.
	ck := cacheKey{templateKey: key}

	err := r.pool.QueryRow(ctx, mustSQL("renderer_resolve.sql"), key, channelID).Scan(&ck.id, &ck.version, &ck.channelID)
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

		// 한 대기자의 취소가 다른 요청을 실패시키지 않으며 공유 DB 조회는 5초로 제한합니다.
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

	// 이미 선택한 행만 읽어 기본값을 공유하는 다른 채널의 override가 섞이지 않게 합니다.
	// 본문 수정은 실제 읽은 버전으로 저장하며 소스 소실만 제한된 재확인을 허용합니다.
	err := r.pool.QueryRow(ctx, mustSQL("renderer_load.sql"), ck.id, ck.templateKey, ck.channelID).Scan(&body, &ck.version)
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

// 실제 본문 버전별로 파싱을 공유하므로 다른 키의 cache 접근은 파싱을 기다리지 않습니다.
func (r *Renderer) parseTemplate(ck cacheKey, body string) (*template.Template, error) {
	value, err, _ := r.parses.Do(fmt.Sprintf("%d/%d", ck.id, ck.version), func() (any, error) {
		if tmpl := r.cachedTemplate(ck); tmpl != nil {
			return tmpl, nil
		}

		tmpl, err := template.New(string(ck.templateKey)).Funcs(templateFuncs).Parse(body)
		if err != nil {
			return nil, fmt.Errorf("parse template: %w", err)
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
