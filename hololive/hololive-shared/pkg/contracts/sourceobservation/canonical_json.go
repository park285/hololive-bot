package sourceobservation

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	CanonicalJSONProfileV1      = "source-observation-canonical-json-v1"
	MaxCanonicalJSONDepth       = 128
	MaxCanonicalJSONSafeInteger = 1<<53 - 1
)

var maxCanonicalSafeIntegerString = strconv.FormatInt(MaxCanonicalJSONSafeInteger, 10)

// canonicalJSONMemberSpan은 출력 버퍼에 이미 기록된 object member 한 개의 위치다.
// Name은 정렬 키로 쓰는 decode된 이름의 names arena 범위이고,
// member는 출력 버퍼 안의 `"name":value` 범위다.
type canonicalJSONMemberSpan struct {
	nameStart, nameEnd     int
	memberStart, memberEnd int
}

// canonicalJSONWriter는 입력 token을 중간 any/map/slice 트리 없이 출력 버퍼로 바로 흘려 쓴다.
// Object만 UTF-16 code unit 순서 정렬을 위해 member 범위를 기록했다가 필요할 때 재배치한다.
// Members와 names는 중첩 object가 stack처럼 쌓고 되돌리는 공유 작업 공간이다.
type canonicalJSONWriter struct {
	decoder  *jsontext.Decoder
	members  []canonicalJSONMemberSpan
	names    []byte
	unquoted []byte
	reorder  []byte
}

// CanonicalizeJSON은 source-observation-canonical-json-v1 규칙으로 단일 JSON 값을 정규화한다.
// 중복 이름, 잘못된 UTF-8, 고립 surrogate, 안전 정수 범위 밖 숫자, 128 초과 중첩, 후행 값은 거부한다.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("payload is empty")
	}

	if len(raw) > MaxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds %d bytes", MaxPayloadBytes)
	}

	// bytes.Buffer 입력은 decoder가 복사 없이 raw를 직접 읽게 한다. decoder는 입력을 쓰지 않는다.
	writer := canonicalJSONWriter{
		decoder: jsontext.NewDecoder(bytes.NewBuffer(raw)),
		reorder: []byte{},
	}

	canonical, err := writer.appendValue(make([]byte, 0, len(raw)), 0)
	if err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}

	if _, readErr := writer.decoder.ReadToken(); !errors.Is(readErr, io.EOF) {
		if readErr == nil {
			return nil, errors.New("decode json: trailing value")
		}

		return nil, fmt.Errorf("decode json trailing data: %w", readErr)
	}

	if len(canonical) > MaxPayloadBytes {
		return nil, fmt.Errorf("canonical payload exceeds %d bytes", MaxPayloadBytes)
	}

	return canonical, nil
}

func (w *canonicalJSONWriter) appendValue(destination []byte, depth int) ([]byte, error) {
	switch w.decoder.PeekKind() {
	case jsontext.KindBeginArray:
		return w.appendArray(destination, depth)
	case jsontext.KindBeginObject:
		return w.appendObject(destination, depth)
	case jsontext.KindInvalid, jsontext.KindNull, jsontext.KindFalse, jsontext.KindTrue,
		jsontext.KindString, jsontext.KindNumber, jsontext.KindEndObject, jsontext.KindEndArray:
	}

	// Peek 오류는 ReadValue가 같은 오류를 다시 보고한다.
	return w.appendScalar(destination)
}

func (w *canonicalJSONWriter) appendScalar(destination []byte) ([]byte, error) {
	value, err := w.decoder.ReadValue()
	if err != nil {
		return nil, fmt.Errorf("read value: %w", err)
	}

	switch value.Kind() {
	case jsontext.KindString:
		return w.appendString(destination, value)
	case jsontext.KindNumber:
		return appendCanonicalJSONNumber(destination, value)
	case jsontext.KindNull, jsontext.KindTrue, jsontext.KindFalse:
		// literal은 decoder가 검증한 정확한 철자 그대로가 정규형이다.
		return append(destination, value...), nil
	case jsontext.KindInvalid, jsontext.KindBeginObject, jsontext.KindEndObject,
		jsontext.KindBeginArray, jsontext.KindEndArray:
	}

	return nil, fmt.Errorf("canonical JSON scalar: unexpected json value %q", value.Kind())
}

// appendString은 escape가 없는 문자열을 원문 그대로 복사한다.
// Decoder가 검증한 escape 없는 JSON 문자열에는 따옴표, 역슬래시, 제어 문자가 없으므로 원문이 곧 정규형이다.
func (w *canonicalJSONWriter) appendString(destination []byte, quoted jsontext.Value) ([]byte, error) {
	if bytes.IndexByte(quoted, '\\') < 0 {
		return append(destination, quoted...), nil
	}

	unquoted, err := jsontext.AppendUnquote(w.unquoted[:0], quoted)
	if err != nil {
		return nil, fmt.Errorf("unquote string: %w", err)
	}

	w.unquoted = unquoted

	return appendCanonicalJSONString(destination, unquoted), nil
}

func (w *canonicalJSONWriter) appendArray(destination []byte, depth int) ([]byte, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}

	if _, err := w.decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("read array start: %w", err)
	}

	destination = append(destination, '[')

	for index := 0; w.decoder.PeekKind() != jsontext.KindEndArray; index++ {
		if index > 0 {
			destination = append(destination, ',')
		}

		var err error

		destination, err = w.appendValue(destination, depth+1)
		if err != nil {
			return nil, err
		}
	}

	if _, err := w.decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("read array end: %w", err)
	}

	return append(destination, ']'), nil
}

// appendObject는 member를 입력 순서대로 출력 버퍼에 먼저 쓰고,
// 입력 순서가 UTF-16 정렬과 다를 때만 해당 object 구간을 재배치한다.
// 재배치는 구간 길이를 바꾸지 않으므로 바깥 object가 기록한 범위는 계속 유효하다.
func (w *canonicalJSONWriter) appendObject(destination []byte, depth int) ([]byte, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}

	if _, err := w.decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("read object start: %w", err)
	}

	objectStart := len(destination)
	membersBase := len(w.members)
	namesBase := len(w.names)

	destination = append(destination, '{')

	for w.decoder.PeekKind() != jsontext.KindEndObject {
		if len(w.members) > membersBase {
			destination = append(destination, ',')
		}

		var err error

		destination, err = w.appendMember(destination, depth)
		if err != nil {
			return nil, err
		}
	}

	if _, err := w.decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("read object end: %w", err)
	}

	destination = append(destination, '}')
	destination = w.sortObjectMembers(destination, objectStart, w.members[membersBase:])

	w.members = w.members[:membersBase]
	w.names = w.names[:namesBase]

	return destination, nil
}

// appendMember의 이름 중복 검사는 decoder 기본 설정(AllowDuplicateNames=false)이 decode된 이름 기준으로 수행한다.
func (w *canonicalJSONWriter) appendMember(destination []byte, depth int) ([]byte, error) {
	quotedName, err := w.decoder.ReadValue()
	if err != nil {
		return nil, fmt.Errorf("read object name: %w", err)
	}

	if len(quotedName) < 2 {
		return nil, errors.New("read object name: missing quoted name")
	}

	nameStart := len(w.names)

	if bytes.IndexByte(quotedName, '\\') < 0 {
		w.names = append(w.names, quotedName[1:len(quotedName)-1]...)
	} else if w.names, err = jsontext.AppendUnquote(w.names, quotedName); err != nil {
		return nil, fmt.Errorf("unquote object name: %w", err)
	}

	nameEnd := len(w.names)
	memberStart := len(destination)

	destination = appendCanonicalJSONString(destination, w.names[nameStart:nameEnd])
	destination = append(destination, ':')

	destination, err = w.appendValue(destination, depth+1)
	if err != nil {
		return nil, err
	}

	w.members = append(w.members, canonicalJSONMemberSpan{
		nameStart: nameStart, nameEnd: nameEnd,
		memberStart: memberStart, memberEnd: len(destination),
	})

	return destination, nil
}

func (w *canonicalJSONWriter) sortObjectMembers(
	destination []byte,
	objectStart int,
	members []canonicalJSONMemberSpan,
) []byte {
	compare := func(left, right canonicalJSONMemberSpan) int {
		return compareJSONNamesUTF16(
			w.names[left.nameStart:left.nameEnd],
			w.names[right.nameStart:right.nameEnd],
		)
	}

	if slices.IsSortedFunc(members, compare) {
		return destination
	}

	slices.SortFunc(members, compare)

	objectSize := len(destination) - objectStart

	w.reorder = slices.Grow(w.reorder[:0], objectSize)[:objectSize]
	copy(w.reorder, destination[objectStart:])

	destination = append(destination[:objectStart], '{')

	for index, member := range members {
		if index > 0 {
			destination = append(destination, ',')
		}

		destination = append(destination, w.reorder[member.memberStart-objectStart:member.memberEnd-objectStart]...)
	}

	return append(destination, '}')
}

// compareJSONNamesUTF16은 유효한 UTF-8 이름을 UTF-16 code unit 사전순으로 비교한다.
// 첫 번째로 다른 code point의 UTF-16 순서가 전체 순서를 결정하므로 변환 버퍼 없이 비교한다.
func compareJSONNamesUTF16(left, right []byte) int {
	for len(left) > 0 && len(right) > 0 {
		leftRune, leftSize := utf8.DecodeRune(left)
		rightRune, rightSize := utf8.DecodeRune(right)

		if leftRune != rightRune {
			return cmp.Compare(utf16CodeUnitOrder(leftRune), utf16CodeUnitOrder(rightRune))
		}

		left, right = left[leftSize:], right[rightSize:]
	}

	return cmp.Compare(len(left), len(right))
}

// utf16CodeUnitOrder는 code point를 UTF-16 code unit 순서와 같은 크기 순서의 키로 바꾼다.
// U+E000..U+FFFF는 보조 평면의 상위 surrogate(0xD800..0xDBFF)보다 뒤에 정렬되어야 한다.
func utf16CodeUnitOrder(character rune) rune {
	if character >= 0xE000 && character <= 0xFFFF {
		return character + utf8.MaxRune + 1
	}

	return character
}

// appendCanonicalJSONString은 decode된 유효 UTF-8 문자열을 JCS 규칙으로 따옴표 처리한다.
// 따옴표, 역슬래시, U+0020 미만 제어 문자만 escape하고 나머지 byte 구간은 그대로 복사한다.
func appendCanonicalJSONString(destination, value []byte) []byte {
	destination = append(destination, '"')

	start := 0

	for index, character := range value {
		if character >= 0x20 && character != '"' && character != '\\' {
			continue
		}

		destination = append(destination, value[start:index]...)
		destination = appendEscapedJSONByte(destination, character)
		start = index + 1
	}

	destination = append(destination, value[start:]...)

	return append(destination, '"')
}

func appendEscapedJSONByte(destination []byte, character byte) []byte {
	const hexadecimal = "0123456789abcdef"

	switch character {
	case '"':
		return append(destination, '\\', '"')
	case '\\':
		return append(destination, '\\', '\\')
	case '\b':
		return append(destination, '\\', 'b')
	case '\t':
		return append(destination, '\\', 't')
	case '\n':
		return append(destination, '\\', 'n')
	case '\f':
		return append(destination, '\\', 'f')
	case '\r':
		return append(destination, '\\', 'r')
	default:
		return append(destination, '\\', 'u', '0', '0', hexadecimal[character>>4], hexadecimal[character&0x0f])
	}
}

// appendCanonicalJSONNumber는 decoder가 검증한 JSON number를 안전 정수 정규형으로 붙인다.
// 소수점/지수가 없는 정수 표기는 문법상 선행 0이 없으므로 -0과 범위만 확인하고 그대로 복사한다.
func appendCanonicalJSONNumber(destination []byte, raw jsontext.Value) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("canonical JSON number is empty")
	}

	if bytes.ContainsAny(raw, ".eE") {
		canonical, err := canonicalizeIntegerJSONNumber(string(raw))
		if err != nil {
			return nil, fmt.Errorf("canonicalize integer JSON number: %w", err)
		}

		return append(destination, canonical...), nil
	}

	if string(raw) == "-0" {
		return append(destination, '0'), nil
	}

	digits := raw
	if digits[0] == '-' {
		digits = digits[1:]
	}

	if exceedsSafeJSONInteger(string(digits)) {
		return nil, errors.New("canonical json integer exceeds the safe range")
	}

	return append(destination, raw...), nil
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
		return "", fmt.Errorf("parse JSON number exponent: %w", err)
	}

	scaled, err := scaleJSONIntegerDigits(digits, exponent-int64(len(fractionalPart)))
	if err != nil {
		return "", fmt.Errorf("scale JSON integer digits: %w", err)
	}

	if exceedsSafeJSONInteger(scaled) {
		return "", errors.New("canonical json integer exceeds the safe range")
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
		return 0, errors.New("canonical json number exponent is outside the accepted range")
	}

	return parsed, nil
}

func scaleJSONIntegerDigits(digits string, scale int64) (string, error) {
	if scale < 0 {
		out, err := shrinkJSONIntegerDigits(digits, -scale)
		if err != nil {
			return out, fmt.Errorf("shrink JSON integer digits: %w", err)
		}

		return out, nil
	}

	if int64(len(digits))+scale > int64(len(maxCanonicalSafeIntegerString)) {
		return "", errors.New("canonical json integer exceeds the safe range")
	}

	return digits + strings.Repeat("0", int(scale)), nil
}

func shrinkJSONIntegerDigits(digits string, fractionalDigits int64) (string, error) {
	if fractionalDigits > int64(len(digits)) {
		return "", errors.New("canonical json numbers must have an integer value")
	}

	integerEnd := len(digits) - int(fractionalDigits)
	if strings.Trim(digits[integerEnd:], "0") != "" {
		return "", errors.New("canonical json numbers must have an integer value")
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
