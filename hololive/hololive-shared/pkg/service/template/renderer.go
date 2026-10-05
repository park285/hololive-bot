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
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

type Renderer struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	cache   map[cacheKey]cacheEntry
	cacheMu sync.RWMutex
	loads   singleflight.Group
	parses  singleflight.Group
}

func NewRenderer(pool *pgxpool.Pool, logger *slog.Logger) *Renderer {
	return &Renderer{
		pool:   pool,
		logger: logger,
		cache:  make(map[cacheKey]cacheEntry),
	}
}

// Render는 DB에서 현재 버전을 확인한 뒤 파싱 결과를 재사용합니다.
// 저장 완료 후 시작한 호출은 새 버전을 사용하며, 저장과 겹친 호출은 이전 버전을 사용할 수 있습니다.
// 템플릿 획득 대기는 최대 5초이며 더 짧은 호출자 deadline을 보존합니다.
// 실행은 호출자 취소와 출력·단계·시간 예산을 따르며 실패하면 부분 결과 없이 오류를 돌려줍니다.
func (r *Renderer) Render(ctx context.Context, key domain.TemplateKey, channelID string, data any) (string, error) {
	tmpl, err := r.getTemplate(ctx, key, channelID)
	if err != nil {
		return "", fmt.Errorf("get template: %w", err)
	}

	return executeTemplate(ctx, tmpl, data)
}

func (r *Renderer) InvalidateCache(key domain.TemplateKey, channelID string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	for ck := range r.cache {
		if ck.templateKey == key && ck.channelID == channelID {
			delete(r.cache, ck)
		}
	}
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

// templateFuncs는 공용 함수와 크기를 늘릴 수 있는 builtin(print·printf·println·html·js·urlquery)의 제한판입니다.
// 결과가 입력보다 커질 수 있는 함수는 모두 파생 값 불변식(renderer_guard.go)을 지키며, 상한 안에서는 원래 함수와
// 같은 결과를 냅니다. Trim·displayline·default·contains·hasPrefix·formatNumber·timeAgo·add는 결과가 입력 크기를
// 넘지 않거나 고정 길이입니다.
var templateFuncs = template.FuncMap{
	"truncate":       boundedTruncate,
	"displayline":    normalizeTemplateDisplayLine,
	"trim":           strings.TrimSpace,
	"upper":          guardStringFunc("upper", strings.ToUpper),
	"lower":          guardStringFunc("lower", strings.ToLower),
	"title":          guardStringFunc("title", toTitle),
	"replace":        boundedReplace,
	"contains":       strings.Contains,
	"hasPrefix":      strings.HasPrefix,
	"join":           boundedJoin,
	"split":          boundedSplit,
	"formatNumber":   formatNumber,
	"formatNumberKR": formatNumberKR,
	"timeAgo":        timeAgo,
	"date":           boundedDate,
	"default":        defaultValue,
	"nl2br":          guardStringFunc("nl2br", nl2br),
	"stripTags":      guardStringFunc("stripTags", stripTags),
	"urlEncode":      guardStringFunc("urlEncode", urlEncode),
	"mdsafe":         guardStringFunc("mdsafe", util.MarkdownNeutralize),
	// 복사할 명령어는 숨은 문자를 삽입하지 않고 Markdown 이스케이프로 보호합니다.
	"mdescape": guardStringFunc("mdescape", kakaoformat.EscapeMarkdown),
	"print":    boundedPrint,
	"printf":   boundedSprintf,
	"println":  boundedPrintln,
	"html":     boundedHTML,
	"js":       boundedJS,
	"urlquery": boundedURLQuery,
	"add":      func(a, b int) int { return a + b },
	"dict":     boundedDict,
}

// toInt64는 다양한 타입의 값을 int64로 변환합니다.
// 값이 nil이거나 변환할 수 없는 타입이면 (0, false)를 반환합니다.
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

// NaN, ±Inf, int64 범위 밖 값은 정수로 바꾸지 않습니다. 호출자는 이 값을 기존과 같이 %v 표기로 출력합니다.
func floatToInt64(f float64) (int64, bool) {
	if math.IsNaN(f) || f >= 0x1p63 || f < -0x1p63 {
		return 0, false
	}

	return int64(f), true
}

func formatNumber(v any) string {
	if v == nil {
		return "0"
	}

	n, ok := toInt64(v)
	if !ok {
		return fmt.Sprintf("%v", v)
	}

	return formatNumberInt64(n)
}

// formatNumberInt64는 세 자리마다 쉼표를 넣습니다. 부호를 문자열에서 분리해 MinInt64도 재귀 없이 처리합니다.
func formatNumberInt64(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""

	if n < 0 {
		sign, digits = "-", digits[1:]
	}

	if len(digits) <= 3 {
		return sign + digits
	}

	var b strings.Builder

	b.Grow(len(sign) + len(digits) + (len(digits)-1)/3)
	b.WriteString(sign)

	head := len(digits) % 3
	if head == 0 {
		head = 3
	}

	b.WriteString(digits[:head])

	for i := head; i < len(digits); i += 3 {
		b.WriteByte(',')
		b.WriteString(digits[i : i+3])
	}

	return b.String()
}

func formatNumberKR(v any) string {
	if v == nil {
		return "0"
	}

	n, ok := toInt64(v)
	if !ok {
		return fmt.Sprintf("%v", v)
	}

	return formatNumberKRInt64(n)
}

// formatNumberKRInt64는 1000 미만은 정수로, 그 이상은 크기를 float64로 나눠 표기합니다. 부호를 float64에서 뒤집어
// MinInt64의 크기(2^63)도 overflow 없이 다룹니다.
func formatNumberKRInt64(n int64) string {
	if n > -1000 && n < 1000 {
		return strconv.FormatInt(n, 10)
	}

	if n < 0 {
		return "-" + formatNumberKRScaled(-float64(n))
	}

	return formatNumberKRScaled(float64(n))
}

func formatNumberKRScaled(magnitude float64) string {
	switch {
	case magnitude >= 100_000_000:
		return fmt.Sprintf("%.1f억", magnitude/100_000_000)
	case magnitude >= 10_000:
		return fmt.Sprintf("%.1f만", magnitude/10_000)
	default:
		return fmt.Sprintf("%.1f천", magnitude/1000)
	}
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < 7*24*time.Hour {
		return formatRecentDuration(d)
	}

	return t.Format(time.DateOnly)
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
