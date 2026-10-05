package template

import (
	"context"
	"errors"
	"fmt"
	"text/template"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// RenderRequest는 RenderBatch의 항목 하나입니다. 채널 override 선택은 Render와 같습니다.
type RenderRequest struct {
	Key       domain.TemplateKey
	ChannelID string
	Data      any
}

// RenderResult는 RenderBatch의 항목별 결과입니다. Err가 있으면 Text는 비어 있습니다.
type RenderResult struct {
	Text string
	Err  error
}

type templateSelector struct {
	key       domain.TemplateKey
	channelID string
}

type templateLookup struct {
	tmpl *template.Template
	err  error
}

// RenderBatch는 모든 항목의 현재 template 버전을 한 SQL 문장으로 확인한 뒤 파싱 결과를 재사용해 렌더링합니다.
// 호출 시작 전에 끝난 저장은 모든 항목에 보이며 저장과 겹친 호출은 이전 버전을 쓸 수 있습니다(Render와 같은 경계).
// 결과는 요청과 같은 순서·길이입니다. Template 부재와 실행 실패는 항목별 Err이며, 버전 조회 실패와 취소는
// 결과 없이 오류를 돌려줍니다. 템플릿 획득 대기는 호출 전체에 최대 5초입니다.
func (r *Renderer) RenderBatch(ctx context.Context, requests []RenderRequest) ([]RenderResult, error) {
	if len(requests) == 0 {
		return []RenderResult{}, nil
	}

	lookups, err := r.getTemplates(ctx, requests)
	if err != nil {
		return nil, fmt.Errorf("get templates: %w", err)
	}

	results := make([]RenderResult, len(requests))

	for i := range requests {
		lookup := lookups[templateSelector{key: requests[i].Key, channelID: requests[i].ChannelID}]
		if lookup.err != nil {
			results[i].Err = fmt.Errorf("get template: %w", lookup.err)

			continue
		}

		text, err := executeTemplate(ctx, lookup.tmpl, requests[i].Data)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("render batch: %w", ctxErr)
			}

			results[i].Err = err

			continue
		}

		results[i].Text = text
	}

	// 실행까지 가지 않은 항목만 있어도 취소된 호출은 결과 없이 실패합니다.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("render batch: %w", err)
	}

	return results, nil
}

func (r *Renderer) getTemplates(ctx context.Context, requests []RenderRequest) (map[templateSelector]templateLookup, error) {
	// Render와 같이 최초 pool 대기부터 제한하며 소스 재확인으로 예산을 연장하지 않습니다.
	ctx, cancel := context.WithTimeout(ctx, templateLoadTimeout)
	defer cancel()

	lookups := make(map[templateSelector]templateLookup, len(requests))
	pending := make([]templateSelector, 0, len(requests))

	for i := range requests {
		selector := templateSelector{key: requests[i].Key, channelID: requests[i].ChannelID}
		if _, seen := lookups[selector]; seen {
			continue
		}

		lookups[selector] = templateLookup{}
		pending = append(pending, selector)
	}

	for attempt := 1; attempt <= templateResolveMaxAttempts && len(pending) > 0; attempt++ {
		resolved, err := r.resolveTemplates(ctx, pending)
		if err != nil {
			return nil, err
		}

		changed := pending[:0:0]

		for index, selector := range pending {
			ck, ok := resolved[index]
			if !ok {
				lookups[selector] = templateLookup{err: fmt.Errorf("%w: %s", ErrTemplateNotFound, selector.key)}

				continue
			}

			tmpl, err := r.loadResolvedTemplate(ctx, ck)

			switch {
			case err == nil:
				lookups[selector] = templateLookup{tmpl: tmpl}
			case errors.Is(err, errTemplateSourceChanged):
				changed = append(changed, selector)

				r.logSourceChange(ctx, selector.key, attempt)
			case ctx.Err() != nil:
				return nil, err
			default:
				lookups[selector] = templateLookup{err: err}
			}
		}

		pending = changed
	}

	for _, selector := range pending {
		lookups[selector] = templateLookup{err: fmt.Errorf("template %s source changed after %d attempts: %w",
			selector.key, templateResolveMaxAttempts, errTemplateSourceChanged)}
	}

	return lookups, nil
}

// resolveTemplates는 선택자마다 채널 override를 기본값보다 우선해 현재 행과 버전을 한 문장으로 고릅니다.
// 반환 map의 key는 selectors의 위치이며 행이 없는 선택자는 빠집니다.
func (r *Renderer) resolveTemplates(ctx context.Context, selectors []templateSelector) (map[int]cacheKey, error) {
	keys := make([]string, len(selectors))
	channels := make([]string, len(selectors))

	for i, selector := range selectors {
		keys[i], channels[i] = string(selector.key), selector.channelID
	}

	rows, err := r.pool.Query(ctx, mustSQL("renderer_resolve_batch.sql"), keys, channels)
	if err != nil {
		return nil, fmt.Errorf("query templates: %w", err)
	}
	defer rows.Close()

	resolved := make(map[int]cacheKey, len(selectors))

	for rows.Next() {
		var (
			ordinality int64
			ck         cacheKey
		)

		if err := rows.Scan(&ordinality, &ck.id, &ck.version, &ck.channelID); err != nil {
			return nil, fmt.Errorf("scan template version: %w", err)
		}

		index := int(ordinality - 1)
		if index < 0 || index >= len(selectors) {
			return nil, fmt.Errorf("scan template version: ordinality %d out of range", ordinality)
		}

		ck.templateKey = selectors[index].key
		resolved[index] = ck
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template versions: %w", err)
	}

	return resolved, nil
}
