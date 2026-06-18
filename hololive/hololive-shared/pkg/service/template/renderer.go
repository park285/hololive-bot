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
	"math"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Renderer struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	cache   map[cacheKey]cacheEntry
	cacheMu sync.RWMutex
}

func NewRenderer(pool *pgxpool.Pool, logger *slog.Logger) *Renderer {
	return &Renderer{
		pool:   pool,
		logger: logger,
		cache:  make(map[cacheKey]cacheEntry),
	}
}

func (r *Renderer) Render(ctx context.Context, key domain.TemplateKey, channelID string, data any) (string, error) {
	tmpl, err := r.getTemplate(ctx, key, channelID)
	if err != nil {
		return "", fmt.Errorf("get template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

func (r *Renderer) getTemplate(ctx context.Context, key domain.TemplateKey, channelID string) (*template.Template, error) {
	ck := cacheKey{templateKey: key, channelID: channelID}

	r.cacheMu.RLock()
	if entry, ok := r.cache[ck]; ok {
		r.cacheMu.RUnlock()
		return entry.tmpl, nil
	}
	r.cacheMu.RUnlock()

	body, err := r.loadTemplateBody(ctx, key, channelID)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New(string(key)).Funcs(templateFuncs).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	r.storeTemplateAt(ck, tmpl, time.Now())

	return tmpl, nil
}

var ErrTemplateNotFound = errors.New("template not found in database")

func (r *Renderer) loadTemplateBody(ctx context.Context, key domain.TemplateKey, channelID string) (string, error) {
	var body string

	if channelID != "" {
		err := r.pool.QueryRow(ctx,
			`SELECT body FROM notification_templates WHERE template_key = $1 AND channel_id = $2`,
			key,
			channelID,
		).Scan(&body)
		if err == nil {
			return body, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("query channel template: %w", err)
		}
	}

	err := r.pool.QueryRow(ctx,
		`SELECT body FROM notification_templates WHERE template_key = $1 AND channel_id IS NULL`,
		key,
	).Scan(&body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("%w: %s", ErrTemplateNotFound, key)
		}
		return "", fmt.Errorf("query default template: %w", err)
	}

	return body, nil
}

func (r *Renderer) InvalidateCache(key domain.TemplateKey, channelID string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	ck := cacheKey{templateKey: key, channelID: channelID}
	delete(r.cache, ck)
}

func (r *Renderer) InvalidateKey(key domain.TemplateKey) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	for ck := range r.cache {
		if ck.templateKey == key {
			delete(r.cache, ck)
		}
	}
}

var templateFuncs = template.FuncMap{
	"truncate": func(maxLen int, s string) string {
		runes := []rune(s)
		if len(runes) <= maxLen {
			return s
		}
		if maxLen <= 3 {
			return string(runes[:maxLen])
		}
		return string(runes[:maxLen-3]) + "..."
	},
	"trim":           strings.TrimSpace,
	"upper":          strings.ToUpper,
	"lower":          strings.ToLower,
	"title":          toTitle,
	"replace":        strings.ReplaceAll,
	"contains":       strings.Contains,
	"hasPrefix":      strings.HasPrefix,
	"join":           strings.Join,
	"split":          strings.Split,
	"formatNumber":   formatNumber,
	"formatNumberKR": formatNumberKR,
	"timeAgo":        timeAgo,
	"date":           formatDate,
	"default":        defaultValue,
	"nl2br":          nl2br,
	"stripTags":      stripTags,
	"urlEncode":      urlEncode,
	"add":            func(a, b int) int { return a + b },
	"dict": func(values ...any) (map[string]any, error) {
		if len(values)%2 != 0 {
			return nil, errors.New("dict requires even number of arguments")
		}
		dict := make(map[string]any, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, errors.New("dict keys must be strings")
			}
			dict[key] = values[i+1]
		}
		return dict, nil
	},
}

// toInt64는 다양한 타입의 값을 int64로 변환합니다.
// nil이거나 변환할 수 없는 타입인 경우 (0, false)를 반환합니다.
func toInt64(v any) (int64, bool) {
	rv, ok := dereferenceValue(v)
	if !ok {
		return 0, false
	}
	return reflectValueToInt64(rv)
}

func dereferenceValue(v any) (reflect.Value, bool) {
	if v == nil {
		return reflect.Value{}, false
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.Value{}, false
		}
		rv = rv.Elem()
	}
	return rv, true
}

func reflectValueToInt64(rv reflect.Value) (int64, bool) {
	kind := rv.Kind()
	if isReflectSignedInt(kind) {
		return rv.Int(), true
	}
	if isReflectUnsignedInt(kind) {
		return uintToInt64(rv.Uint())
	}
	if isReflectFloat(kind) {
		return floatToInt64(rv.Float())
	}
	return 0, false
}

func isReflectSignedInt(kind reflect.Kind) bool {
	return kind == reflect.Int ||
		kind == reflect.Int8 ||
		kind == reflect.Int16 ||
		kind == reflect.Int32 ||
		kind == reflect.Int64
}

func isReflectUnsignedInt(kind reflect.Kind) bool {
	return kind == reflect.Uint ||
		kind == reflect.Uint8 ||
		kind == reflect.Uint16 ||
		kind == reflect.Uint32 ||
		kind == reflect.Uint64
}

func isReflectFloat(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}

func uintToInt64(u uint64) (int64, bool) {
	if u > math.MaxInt64 {
		return 0, false // 오버플로우
	}
	return int64(u), true
}

func floatToInt64(f float64) (int64, bool) {
	if f > math.MaxInt64 || f < math.MinInt64 {
		return 0, false // 오버플로우
	}
	return int64(f), true
}

func formatNumber(v any) string {
	n, ok := toInt64(v)
	if !ok {
		return fmt.Sprintf("%v", v)
	}

	return formatNumberInt64(n)
}

func formatNumberInt64(n int64) string {
	if n < 0 {
		return "-" + formatNumberInt64(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	parts := make([]string, 0, 4)
	for n > 0 {
		parts = append([]string{fmt.Sprintf("%03d", n%1000)}, parts...)
		n /= 1000
	}
	result := strings.Join(parts, ",")
	return strings.TrimLeft(result, "0,")
}

func formatNumberKR(v any) string {
	n, ok := toInt64(v)
	if !ok {
		return fmt.Sprintf("%v", v)
	}

	return formatNumberKRInt64(n)
}

func formatNumberKRInt64(n int64) string {
	if n < 0 {
		return "-" + formatNumberKRInt64(-n)
	}

	switch {
	case n >= 100_000_000:
		return fmt.Sprintf("%.1f억", float64(n)/100_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1f만", float64(n)/10_000)
	case n >= 1000:
		return fmt.Sprintf("%.1f천", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < 7*24*time.Hour {
		return formatRecentDuration(d)
	}
	return t.Format("2006-01-02")
}

func formatRecentDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "방금 전"
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	default:
		return fmt.Sprintf("%d일 전", int(d.Hours()/24))
	}
}

func formatDate(layout string, t time.Time) string {
	return t.Format(layout)
}

func defaultValue(def, val string) string {
	if val == "" {
		return def
	}
	return val
}

func nl2br(s string) string {
	return strings.ReplaceAll(s, "\n", "<br>")
}

func stripTags(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

func toTitle(s string) string {
	if s == "" {
		return ""
	}
	firstRune, size := utf8.DecodeRuneInString(s)
	if firstRune == utf8.RuneError {
		return s
	}
	return strings.ToUpper(string(firstRune)) + s[size:]
}
