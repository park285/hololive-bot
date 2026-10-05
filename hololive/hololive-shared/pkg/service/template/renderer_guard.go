package template

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"time"
)

// fmtMaxPadding은 fmt가 받아들이는 width·precision 최댓값입니다. 더 큰 값은 fmt가 BADWIDTH/BADPREC로 거절합니다.
const fmtMaxPadding = 1_000_000

// fmtVerbOverhead는 verb 하나가 붙이는 오류 표기("%!d(BADPREC)", "%!v(MISSING)" 등)와 따옴표의 여유분입니다.
const fmtVerbOverhead = 32

// 파생 값 불변식: template 함수의 결과 크기(문자열 길이, 그 밖의 값은 measureTemplateValue 상한)는 출력 상한과
// 가장 큰 입력 크기 중 큰 값을 넘지 않습니다. 모든 결과가 이 불변식을 지키므로 template이 만든 값은 64KiB와 가장 큰
// 호출자 데이터 중 큰 값으로 묶이고, 반복 결합·escape·dict 중첩으로 크기를 키울 수 없습니다. 결과가 입력의 상수배를
// 넘을 수 있는 함수는 할당 전에 결과 크기를 계산하고, 나머지는 입력의 상수배 할당 뒤 결과를 확인합니다.
func derivedLimit(maxInputSize int) int {
	return max(templateOutputMaxBytes, maxInputSize)
}

func derivedLimitError(name string, size, limit int) error {
	return fmt.Errorf("%w: %s result %d bytes exceeds %d", ErrTemplateExecutionLimit, name, size, limit)
}

func checkDerivedLen(name string, size, maxInputSize int) error {
	if limit := derivedLimit(maxInputSize); size > limit {
		return derivedLimitError(name, size, limit)
	}

	return nil
}

// guardStringFunc는 결과가 입력의 상수배(UTF-8 보정·escape로 최대 6배)까지 커질 수 있는 문자열 함수의 결과를 제한합니다.
func guardStringFunc(name string, fn func(string) string) func(string) (string, error) {
	return func(s string) (string, error) {
		out := fn(s)
		if err := checkDerivedLen(name, len(out), len(s)); err != nil {
			return "", err
		}

		return out, nil
	}
}

// boundedTruncate는 잘린 앞부분의 잘못된 UTF-8 보정(최대 3배)이 불변식을 넘지 않게 합니다.
func boundedTruncate(maxLen int, s string) (string, error) {
	out := truncateTemplateText(maxLen, s)
	if err := checkDerivedLen("truncate", len(out), len(s)); err != nil {
		return "", err
	}

	return out, nil
}

// boundedDate는 layout 길이에 비례하는 날짜 표기를 layout 크기 기준으로 제한합니다.
func boundedDate(layout string, t time.Time) (string, error) {
	out := formatDate(layout, t)
	if err := checkDerivedLen("date", len(out), len(layout)); err != nil {
		return "", err
	}

	return out, nil
}

// boundedReplace는 치환 결과 크기를 할당 전에 계산해 상한을 넘으면 실패합니다.
func boundedReplace(s, old, replacement string) (string, error) {
	if growth := len(replacement) - len(old); growth > 0 {
		limit := derivedLimit(max(len(s), len(old), len(replacement)))
		// old가 비어 있으면 strings.Count는 ReplaceAll과 같은 삽입 위치 수(rune 수 + 1)를 돌려줍니다.
		if count := strings.Count(s, old); count > 0 && count > (limit-len(s))/growth {
			return "", derivedLimitError("replace", len(s)+count*growth, limit)
		}
	}

	return strings.ReplaceAll(s, old, replacement), nil
}

// boundedSplit는 결과 slice의 출력 크기(원소 + 구분자 + 타입 이름)를 할당 전에 계산합니다. 원소 수도 이 크기로
// 제한되어 slice header 할당이 입력 크기의 상수배를 넘지 않습니다.
func boundedSplit(s, sep string) ([]string, error) {
	// sep가 비어 있으면 strings.Count는 rune 수 + 1을 돌려주며 실제 원소 수보다 하나 많게 계산합니다.
	elements := strings.Count(s, sep) + 1
	if size, limit := len(s)+2*elements+len("[]string")+2, derivedLimit(len(s)); size > limit {
		return nil, derivedLimitError("split", size, limit)
	}

	return strings.Split(s, sep), nil
}

// boundedJoin은 결합 결과 크기를 할당 전에 계산해 상한을 넘으면 실패합니다. 입력 크기는 slice 출력 크기입니다.
func boundedJoin(elems []string, sep string) (string, error) {
	total := 0

	for _, elem := range elems {
		total += len(elem)
	}

	inputSize := max(total+len(elems)+len("[]string")+2, len(sep))
	limit := derivedLimit(inputSize)

	if len(elems) > 1 && len(sep) > 0 && len(elems)-1 > (limit-total)/len(sep) {
		return "", derivedLimitError("join", total+(len(elems)-1)*len(sep), limit)
	}

	return strings.Join(elems, sep), nil
}

// measureTemplateArgs는 인자별 출력 크기 상한의 합·최댓값과 최대 leaf 수를 돌려줍니다.
func measureTemplateArgs(name string, args []any) (total, largest, leaves int, err error) {
	for _, arg := range args {
		size, argLeaves, ok := measureTemplateValue(arg, templateMeasureCeiling)
		if !ok {
			return 0, 0, 0, fmt.Errorf("%w: %s argument exceeds %d bytes", ErrTemplateExecutionLimit, name, templateMeasureCeiling)
		}

		total += size

		largest = max(largest, size)
		leaves = max(leaves, argLeaves)
	}

	return total, largest, leaves, nil
}

// boundedConcat는 print·println·html·js·urlquery처럼 인자를 이어 붙이는 함수의 결과 크기를 인자 측정으로 할당 전에
// 제한하고, escape로 늘어난 결과(최대 6배)도 같은 상한으로 확인합니다. Separators는 인자 사이·끝에 붙는 최대 바이트 수입니다.
func boundedConcat(name string, separators func(args int) int, fn func(...any) string) func(...any) (string, error) {
	return func(args ...any) (string, error) {
		total, largest, _, err := measureTemplateArgs(name, args)
		if err != nil {
			return "", err
		}

		limit := derivedLimit(largest)
		if size := total + separators(len(args)); size > limit {
			return "", derivedLimitError(name, size, limit)
		}

		out := fn(args...)
		if len(out) > limit {
			return "", derivedLimitError(name, len(out), limit)
		}

		return out, nil
	}
}

func betweenArgs(args int) int { return max(args-1, 0) }

func afterEachArg(args int) int { return args }

// boundedDict는 dict 값의 출력 크기 합을 할당 전에 제한합니다. Dict 안에 dict를 거듭 넣어 출력 크기를 두 배씩
// 키우는 구성도 이 상한에서 멈춥니다.
func boundedDict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("dict requires even number of arguments")
	}

	total, largest := len("map[string]interface {}[]"), 0

	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errors.New("dict keys must be strings")
		}

		size, _, ok := measureTemplateValue(values[i+1], templateMeasureCeiling)
		if !ok {
			return nil, fmt.Errorf("%w: dict value exceeds %d bytes", ErrTemplateExecutionLimit, templateMeasureCeiling)
		}

		total += len(key) + size + 2

		largest = max(largest, len(key), size)
	}

	if limit := derivedLimit(largest); total > limit {
		return nil, derivedLimitError("dict", total, limit)
	}

	dict := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		if key, ok := values[i].(string); ok {
			dict[key] = values[i+1]
		}
	}

	return dict, nil
}

// boundedSprintf는 fmt.Sprintf와 같은 결과를 내되, 형식의 verb마다 가장 큰 인자 크기(인자 재사용 %[n] 포함)와
// leaf마다 붙는 width·precision, 쓰이지 않은 인자의 "%!(EXTRA ...)" 표기로 결과 크기 상한을 먼저 계산해 넘으면
// 할당 전에 실패합니다.
func boundedSprintf(format string, args ...any) (string, error) {
	verbs := fmtVerbBound{
		args:      args,
		sizes:     make([]int, len(args)),
		leaves:    make([]int, len(args)),
		maxLeaves: 1,
		// 명시적 인자 번호가 있으면 어느 verb든 어떤 인자도 다시 쓸 수 있어 가장 큰 인자로 계산합니다.
		reordered: strings.IndexByte(format, '[') >= 0,
	}

	for i, arg := range args {
		size, leaves, ok := measureTemplateValue(arg, templateMeasureCeiling)
		if !ok {
			return "", fmt.Errorf("%w: printf argument exceeds %d bytes", ErrTemplateExecutionLimit, templateMeasureCeiling)
		}

		verbs.sizes[i], verbs.leaves[i] = size, max(leaves, 1)
		verbs.maxArgSize = max(verbs.maxArgSize, size)
		verbs.maxLeaves = max(verbs.maxLeaves, leaves)
	}

	limit := derivedLimit(max(len(format), verbs.maxArgSize))

	verbs.budget = limit - len(format)

	if estimate := len(format) + verbs.total(format); estimate > limit {
		return "", derivedLimitError("printf", estimate, limit)
	}

	out := fmt.Sprintf(format, args...)
	if len(out) > limit {
		return "", derivedLimitError("printf", len(out), limit)
	}

	return out, nil
}

// fmtVerbBound는 verb 하나의 출력 상한을 verb 배수 × 인자 크기 + (width + precision + 따옴표) × leaf 수 +
// 오류 표기 여유로 계산합니다. Fmt는 slice·map의 원소마다 width를 적용하므로 leaf 수를 곱합니다. 인자 번호가 없는
// 형식은 fmt와 같은 순서로 인자를 배정하고, 있으면 가장 큰 인자를 씁니다.
type fmtVerbBound struct {
	args       []any
	sizes      []int
	leaves     []int
	maxArgSize int
	maxLeaves  int
	budget     int
	starBound  int
	consumed   int
	starKnown  bool
	reordered  bool
	indexed    bool
}

// total은 모든 verb와 쓰이지 않은 인자 표기 상한의 합을 돌려줍니다. 합이 budget을 넘으면 그 즉시 budget+1을 돌려줍니다.
func (b *fmtVerbBound) total(format string) int {
	total := 0

	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}

		var padding int

		padding, i = b.scanVerb(format, i+1)

		consumes := i < len(format) && format[i] != '%'
		size, leaves := b.verbArg(consumes)

		if consumes {
			b.consumed++
		}

		total += b.verbSize(format, i, padding, size, leaves)
		if total > b.budget {
			return b.budget + 1
		}
	}

	return b.addExtraArgs(total)
}

// verbArg는 verb가 출력할 인자의 크기와 leaf 수입니다. 없는 인자는 "%!d(MISSING)"이며 오류 표기 여유에 포함됩니다.
func (b *fmtVerbBound) verbArg(consumes bool) (size, leaves int) {
	if b.reordered {
		return b.maxArgSize, b.maxLeaves
	}

	if !consumes || b.consumed >= len(b.sizes) {
		return 0, 1
	}

	return b.sizes[b.consumed], b.leaves[b.consumed]
}

// addExtraArgs는 fmt가 순서대로 쓰고 남은 인자를 "%!(EXTRA type=value, ...)"로 덧붙이는 크기를 더합니다.
// Verb 안에 인자 번호를 쓴 형식에는 fmt가 EXTRA를 붙이지 않습니다. 남은 인자만 세므로 정상 형식은 인자 크기를
// 두 번 세지 않습니다.
func (b *fmtVerbBound) addExtraArgs(total int) int {
	if b.indexed || b.consumed >= len(b.args) {
		return total
	}

	total += len("%!(EXTRA )")

	for i := b.consumed; i < len(b.args); i++ {
		total += b.sizes[i] + len(fmt.Sprintf("%T", b.args[i])) + len("=, ")
		if total > b.budget {
			return b.budget + 1
		}
	}

	return total
}

// scanVerb는 fmt.doPrintf와 같은 순서(flag → 인자 번호 → width → '.' → 인자 번호 → precision → 인자 번호)로
// verb 앞부분을 읽어 padding 합과 verb 위치를 돌려줍니다. '*'는 width와 precision 자리에서만 인자를 하나씩 쓰며,
// 그 밖의 문자는 fmt처럼 verb로 다룹니다.
func (b *fmtVerbBound) scanVerb(format string, i int) (padding, verb int) {
	for i < len(format) && strings.IndexByte("#0+- ", format[i]) >= 0 {
		i++
	}

	i, afterIndex := b.argIndex(format, i)

	var value int

	value, i, afterIndex = b.widthOrPrecision(format, i, afterIndex)

	padding += value

	if i+1 < len(format) && format[i] == '.' {
		i, afterIndex = b.argIndex(format, i+1)
		value, i, afterIndex = b.widthOrPrecision(format, i, afterIndex)

		padding += value
	}

	if !afterIndex {
		i, _ = b.argIndex(format, i)
	}

	return padding, i
}

// widthOrPrecision은 '*'(인자 하나를 씀) 또는 숫자를 읽습니다.
func (b *fmtVerbBound) widthOrPrecision(format string, i int, afterIndex bool) (value, next int, stillAfterIndex bool) {
	if i < len(format) && format[i] == '*' {
		value = b.starArg()

		return value, i + 1, false
	}

	value, next = parseFmtNum(format, i)

	return value, next, afterIndex
}

// argIndex는 fmt.argNumber처럼 이 위치의 '['를 인자 번호로 읽습니다. 번호를 쓴 형식은 EXTRA 표기를 생략하고 verb마다
// 가장 큰 인자로 계산합니다. 닫는 괄호가 없으면 fmt처럼 한 글자만 건너뜁니다.
func (b *fmtVerbBound) argIndex(format string, i int) (next int, found bool) {
	if i >= len(format) || format[i] != '[' {
		return i, false
	}

	b.indexed, b.reordered = true, true

	end := strings.IndexByte(format[i+1:], ']')
	if end < 0 {
		return i + 1, false
	}

	digits := format[i+1 : i+1+end]
	number, numberEnd := parseFmtNum(digits, 0)

	return i + end + 2, digits != "" && numberEnd == len(digits) && number <= fmtMaxPadding
}

// parseFmtNum은 fmt.parsenum과 같이 숫자를 읽습니다. 값이 너무 커지면 fmt처럼 형식 끝으로 건너뜁니다.
func parseFmtNum(s string, i int) (value, next int) {
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		if value > fmtMaxPadding {
			return 0, len(s)
		}

		value = value*10 + int(s[i]-'0')
	}

	return value, i
}

// starArg는 '*'가 쓰는 인자의 width 값입니다. 순서대로 배정되는 형식은 실제 인자를, 번호를 쓴 형식은 가장 큰 정수
// 인자를 씁니다. Fmt처럼 인자가 남아 있을 때만 인자 위치를 옮깁니다.
func (b *fmtVerbBound) starArg() int {
	value := b.star()

	if !b.reordered && b.consumed < len(b.args) {
		value = fmtIntArg(b.args[b.consumed])
	}

	if b.consumed < len(b.args) {
		b.consumed++
	}

	return value
}

func (b *fmtVerbBound) verbSize(format string, verb, padding, argSize, argLeaves int) int {
	factor := 1

	if verb < len(format) {
		switch format[verb] {
		case 'q':
			// \xff·\u00e9처럼 바이트당 최대 4바이트로 escape합니다.
			factor = 4
		case 'x', 'X':
			// "% x"는 바이트당 16진수 2자리와 공백을 씁니다.
			factor = 3
		}
	}

	return factor*argSize + (padding+2)*argLeaves + fmtVerbOverhead
}

// star는 '*' width·precision이 받을 수 있는 가장 큰 정수 인자입니다.
func (b *fmtVerbBound) star() int {
	if b.starKnown {
		return b.starBound
	}

	b.starKnown = true

	for _, arg := range b.args {
		b.starBound = max(b.starBound, fmtIntArg(arg))
	}

	return b.starBound
}

// fmtIntArg는 fmt.intFromArg처럼 정수 인자의 절댓값을 width로 씁니다. 정수가 아니거나 fmt 상한을 넘으면 fmt가
// BADWIDTH로 거절해 padding이 없습니다.
func fmtIntArg(arg any) int {
	var n int64

	// fmt는 time.Duration처럼 이름 있는 정수도 허용하므로 구체 타입 대신 Kind를 확인합니다.
	value := reflect.ValueOf(arg)

	if value.CanInt() {
		n = value.Int()
	} else if value.CanUint() {
		n = int64(min(value.Uint(), fmtMaxPadding+1))
	} else {
		return 0
	}

	if n < -fmtMaxPadding || n > fmtMaxPadding {
		return 0
	}

	return int(max(n, -n))
}

var (
	boundedPrint    = boundedConcat("print", betweenArgs, fmt.Sprint)
	boundedPrintln  = boundedConcat("println", afterEachArg, fmt.Sprintln)
	boundedHTML     = boundedConcat("html", betweenArgs, template.HTMLEscaper)
	boundedJS       = boundedConcat("js", betweenArgs, template.JSEscaper)
	boundedURLQuery = boundedConcat("urlquery", betweenArgs, template.URLQueryEscaper)
)
