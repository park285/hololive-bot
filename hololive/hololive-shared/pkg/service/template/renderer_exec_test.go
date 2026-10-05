package template

import (
	"context"
	"errors"
	"maps"
	"math"
	"runtime"
	"strings"
	"testing"
	texttemplate "text/template"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 이름 있는 정수도 fmt의 '*' 인자로 허용되므로 컨테이너 padding 전에 거부해야 합니다.
func TestTemplateNamedIntegerPaddingRejectedBeforeAllocation(t *testing.T) {
	type unsignedWidth uint64

	type pointerWidth uintptr

	for _, width := range []any{time.Duration(1_000_000), unsignedWidth(1_000_000), pointerWidth(1_000_000)} {
		runtime.GC()

		var before, after runtime.MemStats

		runtime.ReadMemStats(&before)

		got, err := boundedRender(t, `{{printf "%*v" .Width .Values}}`, map[string]any{
			"Width": width, "Values": make([]int, 16),
		})

		runtime.ReadMemStats(&after)

		if !errors.Is(err, ErrTemplateExecutionLimit) || got != "" {
			t.Fatalf("width %T: got %d bytes, %v; want execution limit without output", width, len(got), err)
		}

		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 2<<20 {
			t.Errorf("width %T: allocated %d bytes before rejection", width, allocated)
		}
	}
}

func TestFormatNumberIntegerAndFloatEdges(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fn   func(any) string
		in   any
		want string
	}{
		{"min int64", formatNumber, int64(math.MinInt64), "-9,223,372,036,854,775,808"},
		{"max int64", formatNumber, int64(math.MaxInt64), "9,223,372,036,854,775,807"},
		{"negative thousand", formatNumber, -1000, "-1,000"},
		{"negative small", formatNumber, -999, "-999"},
		{"zero", formatNumber, 0, "0"},
		{"million", formatNumber, 1_000_000, "1,000,000"},
		{"partial group", formatNumber, 12345, "12,345"},
		{"float truncates", formatNumber, 1999.9, "1,999"},
		{"float min int64", formatNumber, -0x1p63, "-9,223,372,036,854,775,808"},
		{"float 2^63 not converted", formatNumber, 0x1p63, "9.223372036854776e+18"},
		{"NaN not converted", formatNumber, math.NaN(), "NaN"},
		{"+Inf not converted", formatNumber, math.Inf(1), "+Inf"},
		{"-Inf not converted", formatNumber, math.Inf(-1), "-Inf"},
		{"float32 Inf not converted", formatNumber, float32(math.Inf(1)), "+Inf"},
		{"uint64 overflow not converted", formatNumber, uint64(math.MaxUint64), "18446744073709551615"},
		{"KR min int64", formatNumberKR, int64(math.MinInt64), "-92233720368.5억"},
		{"KR max int64", formatNumberKR, int64(math.MaxInt64), "92233720368.5억"},
		{"KR negative", formatNumberKR, -15000, "-1.5만"},
		{"KR small negative", formatNumberKR, -999, "-999"},
		{"KR NaN not converted", formatNumberKR, math.NaN(), "NaN"},
		{"KR float 2^63 not converted", formatNumberKR, 0x1p63, "9.223372036854776e+18"},
	} {
		if got := tc.fn(tc.in); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

// 관리자 미리보기의 정수·실수 경계 literal은 프로세스를 끝내지 않고 결정적으로 렌더링되어야 합니다.
func TestPreviewNumericEdgeLiteralsTerminate(t *testing.T) {
	t.Parallel()

	service := &AdminService{}

	for body, want := range map[string]string{
		`{{formatNumber -9223372036854775808}}`:    "-9,223,372,036,854,775,808",
		`{{formatNumberKR -9223372036854775808}}`:  "-92233720368.5억",
		`{{formatNumber 9223372036854775808.0}}`:   "9.223372036854776e+18",
		`{{formatNumberKR 9223372036854775808.0}}`: "9.223372036854776e+18",
	} {
		got, _, err := service.Preview(t.Context(), domain.TemplateKeyOutboxShorts, body)
		if err != nil || got != want {
			t.Errorf("Preview(%s) = %q, %v; want %q", body, got, err, want)
		}
	}
}

func TestPreviewCanceledContextProducesNothing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, data, err := (&AdminService{}).Preview(ctx, domain.TemplateKeyOutboxShorts, `{{range 10000}}x{{end}}`)
	if !errors.Is(err, ErrTemplateRenderError) || !errors.Is(err, context.Canceled) || got != "" || data != nil {
		t.Fatalf("Preview(canceled) = %q, %v, %v; want render error with context.Canceled and no output", got, data, err)
	}
}

func boundedRender(t *testing.T, body string, data any) (string, error) {
	t.Helper()

	return boundedRenderContext(t.Context(), t, body, data)
}

func boundedRenderContext(ctx context.Context, t *testing.T, body string, data any) (string, error) {
	t.Helper()

	tmpl, err := parseTemplateBody("bounded", body, false)
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}

	return executeTemplate(ctx, tmpl, data)
}

func TestExecuteTemplateCancellationMidRunHasNoPartialOutput(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	data := map[string]any{"Cancel": func() string {
		cancel()

		return ""
	}}

	got, err := boundedRenderContext(ctx, t, `{{range 10}}x{{call $.Cancel}}{{end}}`, data)
	if !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("got %q, %v; want context.Canceled without partial output", got, err)
	}
}

func TestExecuteTemplateBudgetsFailWithoutPartialOutput(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("가\n", 50_000)

	for name, tc := range map[string]struct {
		body string
		data any
	}{
		"output over limit":         {body: `{{range 20}}{{printf "%5000s" ""}}{{end}}`},
		"large data output":         {body: `{{.}}`, data: big},
		"no-output integer loop":    {body: `{{range 1000000000}}{{end}}`},
		"no-output nested loops":    {body: `{{range 100000}}{{range 100000}}{{end}}{{end}}`},
		"no-output assignment loop": {body: `{{$x := 0}}{{range 1000000000}}{{$x = add $x 1}}{{end}}`},
		"exponential shallow calls": {body: `{{define "a"}}{{if lt . 20}}{{template "a" (add . 1)}}{{template "a" (add . 1)}}{{end}}{{end}}{{template "a" 0}}`},
		"unbounded recursion":       {body: `{{define "a"}}{{template "a" .}}{{end}}{{template "a" .}}`},
		"printf width":              {body: `{{$x := printf "%1000000d" 1}}`},
		"printf star width":         {body: `{{range 100}}{{$x := printf "%*d%*d" 60000 1 60000 2}}{{end}}`},
		"replace growth":            {body: `{{$s := "ab"}}{{range 40}}{{$s = replace $s "" $s}}{{end}}{{len $s}}`},
		"replace growth on data":    {body: `{{len (replace . "\n" "\n\u200b")}}`, data: big},
		"split growth":              {body: `{{len (split (printf "%60000s" "") "")}}`},
		"join separator growth":     {body: `{{len (join (split "a,b,c" ",") (printf "%40000s" ""))}}`},
		"print concat":              {body: `{{$s := printf "%30000s" ""}}{{len (print $s $s $s)}}`},
		"html escape chain":         {body: `{{$s := "<>&"}}{{range 30}}{{$s = html $s $s}}{{end}}{{len $s}}`},
		"mdescape concat chain":     {body: `{{$s := "\\*"}}{{range 30}}{{$s = mdescape (print $s $s)}}{{end}}{{len $s}}`},
	} {
		start := time.Now()

		got, err := boundedRender(t, tc.body, tc.data)
		if !errors.Is(err, ErrTemplateExecutionLimit) || got != "" {
			t.Errorf("%s: got %d bytes, %v; want ErrTemplateExecutionLimit without output", name, len(got), err)
		}

		if elapsed := time.Since(start); elapsed > templateExecTimeout+time.Second {
			t.Errorf("%s: budget enforcement took %s", name, elapsed)
		}
	}
}

// 증폭 template은 큰 버퍼를 만들기 전에 거절되어야 합니다. 병렬 테스트와 겹치지 않도록 순차 실행해 TotalAlloc 증가량을
// 잽니다. 수정 전 같은 본문은 22–42MB를 할당하거나(UTF-8 보정 반복은 531441바이트 결과로) 성공했습니다.
func TestTemplateAmplificationRejectedBeforeLargeAllocation(t *testing.T) {
	const allocationBudget = 2 << 20

	for name, body := range map[string]string{
		"printf argument reuse":     `{{$b := printf "%032000d" 0}}{{$f := replace (printf "%0200d" 0) "0" "%[1]s"}}{{len (printf $f $b)}}`,
		"printf composite padding":  `{{len (printf "%1000v" (split (printf "%010000d" 0) ""))}}`,
		"dict DAG print":            `{{$d := dict "k" (printf "%01000d" 0)}}{{range 12}}{{$d = dict "a" $d "b" $d}}{{end}}{{len (print $d)}}`,
		"dict DAG bare action":      `{{$d := dict "k" (printf "%01000d" 0)}}{{range 12}}{{$d = dict "a" $d "b" $d}}{{end}}{{$d}}`,
		"upper UTF-8 ratchet":       `{{$s := "\xff"}}{{range 12}}{{$s = replace (upper $s) "\uFFFD" "\xff\xff\xff"}}{{end}}{{len $s}}`,
		"lower UTF-8 ratchet":       `{{$s := "\xff"}}{{range 12}}{{$s = replace (lower $s) "\uFFFD" "\xff\xff\xff"}}{{end}}{{len $s}}`,
		"stripTags UTF-8 ratchet":   `{{$s := "\xff"}}{{range 12}}{{$s = replace (stripTags $s) "\uFFFD" "\xff\xff\xff"}}{{end}}{{len $s}}`,
		"truncate UTF-8 ratchet":    `{{$s := "\xff\xff\xff\xff\xff"}}{{range 12}}{{$s = replace (truncate (add (len $s) -1) $s) "\uFFFD" "\xff\xff\xff"}}{{end}}{{len $s}}`,
		"printf unused arguments":   `{{$b := printf "%032000d" 0}}{{len (printf "" ` + strings.Repeat("$b ", 100) + `)}}`,
		"printf star as verb extra": `{{$b := printf "%032000d" 0}}{{len (printf "%` + strings.Repeat("*", 100) + `" 0 0 ` + strings.Repeat("$b ", 98) + `)}}`,
	} {
		runtime.GC()

		var before, after runtime.MemStats

		runtime.ReadMemStats(&before)

		got, _, err := (&AdminService{}).Preview(t.Context(), domain.TemplateKeyOutboxShorts, body)

		runtime.ReadMemStats(&after)

		if !errors.Is(err, ErrTemplateExecutionLimit) || got != "" {
			t.Errorf("%s: got %q, %v; want ErrTemplateExecutionLimit without output", name, got, err)
		}

		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > allocationBudget {
			t.Errorf("%s: allocated %d bytes before rejection; want at most %d", name, allocated, allocationBudget)
		}
	}
}

// 본문 상한은 한 action 안에서 단계 확인 없이 실행되는 호출 수를 제한하므로 저장·미리보기 모두 넘는 본문을 거절합니다.
func TestTemplateSourceOverLimitRejected(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("x", templateSourceMaxBytes+1)

	got, _, err := (&AdminService{}).Preview(t.Context(), domain.TemplateKeyOutboxShorts, body)
	if !errors.Is(err, ErrTemplateParseError) || !errors.Is(err, ErrTemplateExecutionLimit) || got != "" {
		t.Fatalf("Preview(over limit) = %d bytes, %v; want parse error with ErrTemplateExecutionLimit", len(got), err)
	}

	if _, err := parseTemplateBody("exact", strings.Repeat("x", templateSourceMaxBytes), false); err != nil {
		t.Fatalf("parse at limit: %v", err)
	}
}

// 계측 지점과 제한판 builtin은 정상 template의 출력 바이트를 바꾸지 않아야 합니다.
func TestBoundedExecutionPreservesValidConstructs(t *testing.T) {
	t.Parallel()

	reference := maps.Clone(templateFuncs)

	for _, builtin := range []string{"print", "printf", "println", "html", "js", "urlquery"} {
		delete(reference, builtin)
	}

	data := map[string]any{"A": "x", "Empty": "", "N": 3, "L": []string{"a", "b", "c", "d"}, "Width": time.Duration(-4)}

	for _, body := range []string{
		"{{- if .A}} a {{- else}} b {{- end}}\n{{range $i, $e := .L}}{{if $i}}, {{end}}{{$e}}{{if eq $e \"c\"}}{{break}}{{end}}{{end}}",
		`{{define "item"}}[{{.}}]{{end}}{{range .L}}{{template "item" .}}{{end}}{{block "tail" .A}}<{{.}}>{{end}}`,
		`{{define "c"}}({{.}}){{end}}{{define "b"}}{{template "c" .}}{{end}}{{define "a"}}{{template "b" .}}{{end}}{{template "a" .N}}`,
		`{{with .Empty}}{{.}}{{else with .A}}{{.}}{{else}}none{{end}}{{range 3}}{{continue}}{{end}}{{range .N}}{{.}}{{end}}`,
		`{{printf "%02d/%02d" 3 7}}{{printf "\u200b"}}{{printf "%s%s %s" "!" "cmd" "x"}}{{printf "%[2]s-%[1]s" "a" "b"}}{{printf "%-4s|%5.1f" "ab" 3.14159}}`,
		`{{print "a" 1 2 "b"}}{{println 1 "x"}}{{html "<a&>" 1}}{{js "it's"}}{{urlquery "a b&c"}}`,
		`{{join (split "a,b,c" ",") " / "}}{{replace "a\nb" "\n" "\n\u200b"}}{{truncate 5 "가나다라마바사"}}{{formatNumber 1234567}}`,
		`{{$d := dict "a" 1 "b" "x" "c" .L}}{{$d.a}}{{index $d "b"}}{{print $d}}{{$d}}{{printf "%v|%5v|%q" (split "a,b" ",") (split "a,b" ",") $d.c}}`,
		`{{upper "abc"}}{{lower "ABC"}}{{title "abc"}}{{stripTags "<b>x</b>"}}{{printf "%q %x %T %*d" "a\"b" "hi" 1 4 7}}{{mdescape "*x*"}}{{urlEncode "a b"}}{{nl2br "a\nb"}}`,
		`{{printf "%s" "a" "b" 3}}{{printf "%d %d" 1}}{{printf "%[1]s" "a" "b"}}{{printf "%!" 1}}{{printf "100%%" 1}}`,
		`{{printf "%*d|%-*d|%.*f|%*.*f" 3 1 3 2 2 3.14159 6 2 2.5}}{{printf "%**d" 1 2 3}}{{printf "%5*d" 1 2}}{{printf "%[x]d %[2" 1 2}}{{printf "%." 1}}{{printf "%*" 4}}`,
		`{{printf "%*s|%.*f" .Width "x" .Width 3.14159}}`,
	} {
		raw, err := texttemplate.New("raw").Funcs(reference).Parse(body)
		if err != nil {
			t.Fatalf("parse reference %q: %v", body, err)
		}

		var want strings.Builder

		if execErr := raw.Execute(&want, data); execErr != nil {
			t.Fatalf("execute reference %q: %v", body, execErr)
		}

		got, err := boundedRender(t, body, data)
		if err != nil || got != want.String() {
			t.Errorf("bounded %q = %q, %v; want %q", body, got, err, want.String())
		}
	}
}

// 출력 한도 가까운 정상 printf·print는 사전 상한 계산 때문에 거절되지 않아야 합니다.
func TestBoundedExecutionAllowsNearLimitFormatting(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{{printf "%s" (printf "%60000s" "")}}`,
		`{{printf "%s|%d" (printf "%60000s" "") 7}}`,
		`{{print (printf "%60000s" "")}}`,
	} {
		got, err := boundedRender(t, body, nil)
		if err != nil || len(got) < 60000 {
			t.Errorf("%s: got %d bytes, %v; want near-limit output", body, len(got), err)
		}
	}
}

// 출력 한도보다 긴 데이터도 줄이는 처리 뒤 짧게 출력하면 기존처럼 성공해야 합니다.
func TestBoundedExecutionAllowsShrinkingLargeData(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("가\n", 50_000)

	got, err := boundedRender(t, `{{truncate 10 (displayline (replace (replace . "\n" " ") "가" "a"))}}`, big)
	if err != nil || got != "a a a a..." {
		t.Fatalf("got %q, %v; want truncated output", got, err)
	}
}
