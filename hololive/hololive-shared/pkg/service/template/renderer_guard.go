package template

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"time"
)

const fmtMaxPadding = 1_000_000

const fmtVerbOverhead = 32

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

func guardStringFunc(name string, fn func(string) string) func(string) (string, error) {
	return func(s string) (string, error) {
		out := fn(s)
		if err := checkDerivedLen(name, len(out), len(s)); err != nil {
			return "", err
		}

		return out, nil
	}
}

func boundedTruncate(maxLen int, s string) (string, error) {
	out := truncateTemplateText(maxLen, s)
	if err := checkDerivedLen("truncate", len(out), len(s)); err != nil {
		return "", err
	}

	return out, nil
}

func boundedDate(layout string, t time.Time) (string, error) {
	out := formatDate(layout, t)
	if err := checkDerivedLen("date", len(out), len(layout)); err != nil {
		return "", err
	}

	return out, nil
}

func boundedReplace(s, old, replacement string) (string, error) {
	if growth := len(replacement) - len(old); growth > 0 {
		limit := derivedLimit(max(len(s), len(old), len(replacement)))
		if count := strings.Count(s, old); count > 0 && count > (limit-len(s))/growth {
			return "", derivedLimitError("replace", len(s)+count*growth, limit)
		}
	}

	return strings.ReplaceAll(s, old, replacement), nil
}

func boundedSplit(s, sep string) ([]string, error) {
	elements := strings.Count(s, sep) + 1
	if size, limit := len(s)+2*elements+len("[]string")+2, derivedLimit(len(s)); size > limit {
		return nil, derivedLimitError("split", size, limit)
	}

	return strings.Split(s, sep), nil
}

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

func boundedSprintf(format string, args ...any) (string, error) {
	verbs := fmtVerbBound{
		args:      args,
		sizes:     make([]int, len(args)),
		leaves:    make([]int, len(args)),
		maxLeaves: 1,
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

func (b *fmtVerbBound) verbArg(consumes bool) (size, leaves int) {
	if b.reordered {
		return b.maxArgSize, b.maxLeaves
	}

	if !consumes || b.consumed >= len(b.sizes) {
		return 0, 1
	}

	return b.sizes[b.consumed], b.leaves[b.consumed]
}

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

func (b *fmtVerbBound) widthOrPrecision(format string, i int, afterIndex bool) (value, next int, stillAfterIndex bool) {
	if i < len(format) && format[i] == '*' {
		value = b.starArg()

		return value, i + 1, false
	}

	value, next = parseFmtNum(format, i)

	return value, next, afterIndex
}

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

func parseFmtNum(s string, i int) (value, next int) {
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		if value > fmtMaxPadding {
			return 0, len(s)
		}

		value = value*10 + int(s[i]-'0')
	}

	return value, i
}

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
			factor = 4
		case 'x', 'X':
			factor = 3
		}
	}

	return factor*argSize + (padding+2)*argLeaves + fmtVerbOverhead
}

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

func fmtIntArg(arg any) int {
	var n int64

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
