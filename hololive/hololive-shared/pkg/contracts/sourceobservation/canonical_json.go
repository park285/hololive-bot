package sourceobservation

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	CanonicalJSONProfileV1      = "source-observation-canonical-json-v1"
	MaxCanonicalJSONDepth       = 128
	MaxCanonicalJSONSafeInteger = 1<<53 - 1
)

var maxCanonicalSafeIntegerString = strconv.FormatInt(MaxCanonicalJSONSafeInteger, 10)

type canonicalJSONMember struct {
	name  string
	order []uint16
	value any
}

type canonicalJSONNumber string

func CanonicalizeJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("payload is empty")
	}
	if len(raw) > MaxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds %d bytes", MaxPayloadBytes)
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	value, err := readCanonicalJSONValue(decoder, 0)
	if err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode json: trailing value")
		}
		return nil, fmt.Errorf("decode json trailing data: %w", err)
	}
	canonical, err := appendCanonicalJSON(make([]byte, 0, len(raw)), value, 0)
	if err != nil {
		return nil, err
	}
	if len(canonical) > MaxPayloadBytes {
		return nil, fmt.Errorf("canonical payload exceeds %d bytes", MaxPayloadBytes)
	}
	return canonical, nil
}

func readCanonicalJSONValue(decoder *jsontext.Decoder, depth int) (any, error) {
	token, err := decoder.ReadToken()
	if err != nil {
		return nil, err
	}

	kind := token.Kind()
	if kind == jsontext.KindBeginArray {
		return readCanonicalJSONArray(decoder, depth)
	}
	if kind == jsontext.KindBeginObject {
		return readCanonicalJSONObject(decoder, depth)
	}
	return canonicalJSONScalar(token)
}

func canonicalJSONScalar(token jsontext.Token) (any, error) {
	kind := token.Kind()
	if kind == jsontext.KindNull {
		return nil, nil
	}
	if kind == jsontext.KindTrue || kind == jsontext.KindFalse {
		return token.Bool(), nil
	}
	if kind == jsontext.KindString {
		return token.String(), nil
	}
	if kind == jsontext.KindNumber {
		return canonicalJSONNumber(token.String()), nil
	}
	return nil, fmt.Errorf("unexpected json token %q", kind)
}

func readCanonicalJSONArray(decoder *jsontext.Decoder, depth int) ([]any, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}

	values := make([]any, 0)
	for decoder.PeekKind() != jsontext.KindEndArray {
		value, err := readCanonicalJSONValue(decoder, depth+1)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	return values, nil
}

func readCanonicalJSONObject(decoder *jsontext.Decoder, depth int) (map[string]any, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}

	values := make(map[string]any)
	for decoder.PeekKind() != jsontext.KindEndObject {
		name, value, err := readCanonicalJSONField(decoder, depth)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	return values, nil
}

func readCanonicalJSONField(decoder *jsontext.Decoder, depth int) (name string, value any, err error) {
	nameToken, err := decoder.ReadToken()
	if err != nil {
		return "", nil, err
	}
	if nameToken.Kind() != jsontext.KindString {
		return "", nil, fmt.Errorf("object field name is not a string")
	}
	name = nameToken.String()
	value, err = readCanonicalJSONValue(decoder, depth+1)
	if err != nil {
		return "", nil, err
	}
	return name, value, nil
}

func appendCanonicalJSON(destination []byte, value any, depth int) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(destination, "null"...), nil
	case bool:
		return appendCanonicalJSONBool(destination, typed), nil
	case string:
		return appendCanonicalJSONString(destination, typed), nil
	default:
		return appendCanonicalJSONNumberOrComposite(destination, value, depth)
	}
}

func appendCanonicalJSONBool(destination []byte, value bool) []byte {
	if value {
		return append(destination, "true"...)
	}
	return append(destination, "false"...)
}

func appendCanonicalJSONNumberOrComposite(destination []byte, value any, depth int) ([]byte, error) {
	switch typed := value.(type) {
	case canonicalJSONNumber:
		return appendCanonicalJSONNumber(destination, typed)
	case []any:
		return appendCanonicalJSONArray(destination, typed, depth)
	case map[string]any:
		return appendCanonicalJSONObject(destination, typed, depth)
	default:
		return nil, fmt.Errorf("canonical json contains unsupported value type")
	}
}

func appendCanonicalJSONNumber(destination []byte, value canonicalJSONNumber) ([]byte, error) {
	canonical, err := canonicalizeIntegerJSONNumber(string(value))
	if err != nil {
		return nil, err
	}
	return append(destination, canonical...), nil
}

func appendCanonicalJSONArray(destination []byte, values []any, depth int) ([]byte, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}
	destination = append(destination, '[')
	for index := range values {
		if index > 0 {
			destination = append(destination, ',')
		}
		var err error
		destination, err = appendCanonicalJSON(destination, values[index], depth+1)
		if err != nil {
			return nil, err
		}
	}
	return append(destination, ']'), nil
}

func appendCanonicalJSONObject(destination []byte, values map[string]any, depth int) ([]byte, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}
	members := canonicalObjectMembers(values)
	destination = append(destination, '{')
	for index := range members {
		if index > 0 {
			destination = append(destination, ',')
		}
		destination = appendCanonicalJSONString(destination, members[index].name)
		destination = append(destination, ':')
		var err error
		destination, err = appendCanonicalJSON(destination, members[index].value, depth+1)
		if err != nil {
			return nil, err
		}
	}
	return append(destination, '}'), nil
}

func canonicalObjectMembers(values map[string]any) []canonicalJSONMember {
	members := make([]canonicalJSONMember, 0, len(values))
	for name, memberValue := range values {
		members = append(members, canonicalJSONMember{
			name: name, order: utf16.Encode([]rune(name)), value: memberValue,
		})
	}
	sort.Slice(members, func(i, j int) bool {
		return lessUTF16(members[i].order, members[j].order)
	})
	return members
}

func appendCanonicalJSONString(destination []byte, value string) []byte {
	destination = append(destination, '"')
	for _, character := range value {
		destination = appendEscapedJSONRune(destination, character)
	}
	return append(destination, '"')
}

func appendEscapedJSONRune(destination []byte, character rune) []byte {
	switch character {
	case '"':
		return append(destination, '\\', '"')
	case '\\':
		return append(destination, '\\', '\\')
	default:
		return appendEscapedJSONControl(destination, character)
	}
}

func appendEscapedJSONControl(destination []byte, character rune) []byte {
	switch character {
	case '\b':
		return append(destination, '\\', 'b')
	case '\t':
		return append(destination, '\\', 't')
	case '\n':
		return append(destination, '\\', 'n')
	default:
		return appendEscapedJSONFormOrCarriageReturn(destination, character)
	}
}

func appendEscapedJSONFormOrCarriageReturn(destination []byte, character rune) []byte {
	switch character {
	case '\f':
		return append(destination, '\\', 'f')
	case '\r':
		return append(destination, '\\', 'r')
	default:
		return appendJSONControlOrRune(destination, character)
	}
}

func appendJSONControlOrRune(destination []byte, character rune) []byte {
	const hexadecimal = "0123456789abcdef"
	if character < 0x20 {
		return append(
			destination,
			'\\', 'u', '0', '0',
			hexadecimal[character>>4],
			hexadecimal[character&0x0f],
		)
	}
	return utf8.AppendRune(destination, character)
}

func lessUTF16(left, right []uint16) bool {
	limit := min(len(left), len(right))
	for index := range limit {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return len(left) < len(right)
}

func canonicalizeIntegerJSONNumber(raw string) (string, error) {
	negative := strings.HasPrefix(raw, "-")
	unsigned, exponentText, fractionalPart := splitJSONNumber(raw)
	digits := strings.TrimLeft(unsigned+fractionalPart, "0")
	if digits == "" {
		return "0", nil
	}
	exponent, err := parseJSONNumberExponent(exponentText)
	if err != nil {
		return "", err
	}
	scaled, err := scaleJSONIntegerDigits(digits, exponent-int64(len(fractionalPart)))
	if err != nil {
		return "", err
	}
	if exceedsSafeJSONInteger(scaled) {
		return "", fmt.Errorf("canonical json integer exceeds the safe range")
	}
	if negative {
		return "-" + scaled, nil
	}
	return scaled, nil
}

func splitJSONNumber(raw string) (unsigned, exponentText, fractionalPart string) {
	unsigned = strings.TrimPrefix(raw, "-")
	if exponentIndex := strings.IndexAny(unsigned, "eE"); exponentIndex >= 0 {
		exponentText = unsigned[exponentIndex+1:]
		unsigned = unsigned[:exponentIndex]
	}
	if decimalIndex := strings.IndexByte(unsigned, '.'); decimalIndex >= 0 {
		fractionalPart = unsigned[decimalIndex+1:]
		unsigned = unsigned[:decimalIndex]
	}
	return unsigned, exponentText, fractionalPart
}

func parseJSONNumberExponent(exponentText string) (int64, error) {
	if exponentText == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(exponentText, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("canonical json number exponent is outside the accepted range")
	}
	return parsed, nil
}

func scaleJSONIntegerDigits(digits string, scale int64) (string, error) {
	if scale < 0 {
		return shrinkJSONIntegerDigits(digits, -scale)
	}
	if int64(len(digits))+scale > int64(len(maxCanonicalSafeIntegerString)) {
		return "", fmt.Errorf("canonical json integer exceeds the safe range")
	}
	return digits + strings.Repeat("0", int(scale)), nil
}

func shrinkJSONIntegerDigits(digits string, fractionalDigits int64) (string, error) {
	if fractionalDigits > int64(len(digits)) {
		return "", fmt.Errorf("canonical json numbers must have an integer value")
	}
	integerEnd := len(digits) - int(fractionalDigits)
	if strings.Trim(digits[integerEnd:], "0") != "" {
		return "", fmt.Errorf("canonical json numbers must have an integer value")
	}
	digits = strings.TrimLeft(digits[:integerEnd], "0")
	if digits == "" {
		return "0", nil
	}
	return digits, nil
}

func exceedsSafeJSONInteger(digits string) bool {
	return len(digits) > len(maxCanonicalSafeIntegerString) ||
		len(digits) == len(maxCanonicalSafeIntegerString) && digits > maxCanonicalSafeIntegerString
}
