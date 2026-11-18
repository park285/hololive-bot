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
	"github.com/kapu/hololive-shared/pkg/kakaotext"
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

func (r *Renderer) Render(ctx context.Context, key domain.TemplateKey, channelID string, format domain.TemplateFormat, data any) (string, error) {
	tmpl, err := r.getTemplate(ctx, key, channelID, format)
	if err != nil {
		return "", fmt.Errorf("get template: %w", err)
	}

	return executeTemplate(ctx, tmpl, data)
}

var templateFuncs = template.FuncMap{
	"truncate":       boundedTruncate,
	"displayline":    normalizeTemplateDisplayLine,
	"trustedlink":    trustedLink,
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
	"mdsafe":         guardStringFunc("mdsafe", kakaotext.MarkdownLiteral),
	"mdescape":       guardStringFunc("mdescape", kakaoformat.EscapeMarkdown),
	"print":          boundedPrint,
	"printf":         boundedSprintf,
	"println":        boundedPrintln,
	"html":           boundedHTML,
	"js":             boundedJS,
	"urlquery":       boundedURLQuery,
	"add":            func(a, b int) int { return a + b },
	"dict":           boundedDict,
}

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
		return 0, false
	}

	return int64(u), true
}

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

func formatNumberKRInt64(n int64) string {
	if n > -1000 && n < 1000 {
		return strconv.FormatInt(n, 10)
	}

	sign := ""

	if n < 0 {
		sign = "-"
	}

	if tenths := roundedTenths(n, 1000); tenths < 100 {
		return sign + formatTenths(tenths, "천")
	}

	if tenths := roundedTenths(n, 10_000); tenths < 100_000 {
		return sign + formatTenths(tenths, "만")
	}

	return sign + formatTenths(roundedTenths(n, 100_000_000), "억")
}

func roundedTenths(n, unit int64) int64 {
	step := unit / 10
	quotient, remainder := n/step, n%step

	if n < 0 {
		quotient, remainder = -quotient, -remainder
	}

	if remainder*2 >= step {
		quotient++
	}

	return quotient
}

func formatTenths(tenths int64, suffix string) string {
	return strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10) + suffix
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
