package template

import (
	"context"
	"errors"
	"fmt"
	"text/template"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type RenderRequest struct {
	Key       domain.TemplateKey
	ChannelID string
	Format    domain.TemplateFormat
	Data      any
}

type RenderResult struct {
	Text string
	Err  error
}

type templateSelector struct {
	key       domain.TemplateKey
	channelID string
	format    domain.TemplateFormat
}

type templateLookup struct {
	tmpl *template.Template
	err  error
}

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
		lookup := lookups[requests[i].selector()]
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

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("render batch: %w", err)
	}

	return results, nil
}

func (request RenderRequest) selector() templateSelector {
	return templateSelector{key: request.Key, channelID: request.ChannelID, format: request.Format}
}

func (r *Renderer) getTemplates(ctx context.Context, requests []RenderRequest) (map[templateSelector]templateLookup, error) {
	ctx, cancel := context.WithTimeout(ctx, templateLoadTimeout)
	defer cancel()

	lookups := make(map[templateSelector]templateLookup, len(requests))
	pending := make([]templateSelector, 0, len(requests))

	for i := range requests {
		selector := requests[i].selector()
		if _, seen := lookups[selector]; seen {
			continue
		}

		if !selector.format.IsValid() {
			lookups[selector] = templateLookup{err: fmt.Errorf("%w: %q", ErrInvalidTemplateFormat, selector.format)}

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

func (r *Renderer) resolveTemplates(ctx context.Context, selectors []templateSelector) (map[int]cacheKey, error) {
	keys := make([]string, len(selectors))
	channels := make([]string, len(selectors))
	formats := make([]string, len(selectors))

	for i, selector := range selectors {
		keys[i], channels[i], formats[i] = string(selector.key), selector.channelID, string(selector.format)
	}

	rows, err := pgxdb.Query(ctx, r.pool, rendererResolveBatchSQL, keys, channels, formats)
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
