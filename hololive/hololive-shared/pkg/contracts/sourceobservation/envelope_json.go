package sourceobservation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

func SHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func decodeStrictJSON(raw []byte, destination any) error {
	if err := validateJSONText(raw); err != nil {
		return err
	}
	if err := rejectDuplicateJSONNames(raw); err != nil {
		return err
	}
	if err := rejectNonCanonicalJSONFields(raw, reflect.TypeOf(destination)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == nil {
		return fmt.Errorf("decode json: trailing value")
	}
	if err != io.EOF {
		return fmt.Errorf("decode json trailing data: %w", err)
	}
	return nil
}

func rejectDuplicateJSONNames(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder, 0); err != nil {
		return fmt.Errorf("validate json structure: %w", err)
	}
	return ensureJSONEOF(decoder)
}

var (
	timeType       = reflect.TypeFor[time.Time]()
	rawMessageType = reflect.TypeFor[json.RawMessage]()
)

func rejectNonCanonicalJSONFields(raw []byte, destinationType reflect.Type) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := inspectJSONValueWithType(decoder, indirectJSONType(destinationType)); err != nil {
		return fmt.Errorf("validate json fields: %w", err)
	}
	return ensureJSONEOF(decoder)
}

func inspectJSONValueWithType(decoder *json.Decoder, valueType reflect.Type) error {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	return inspectJSONDelimiter(decoder, delimiter, valueType)
}

func inspectJSONDelimiter(decoder *json.Decoder, delimiter json.Delim, valueType reflect.Type) error {
	if valueType == nil {
		return consumeJSONContainer(decoder, delimiter)
	}
	if delimiter == '{' {
		return inspectJSONObjectFields(decoder, valueType)
	}
	if delimiter == '[' {
		return inspectJSONArrayFields(decoder, valueType)
	}
	return fmt.Errorf("unexpected delimiter %q", delimiter)
}

func inspectJSONObjectFields(decoder *json.Decoder, valueType reflect.Type) error {
	valueType = indirectJSONType(valueType)
	if skipTypedJSONObject(valueType) {
		return consumeJSONContainer(decoder, '{')
	}
	fields := jsonFieldTypes(valueType)
	seen := make(map[string]struct{}, len(fields))
	for decoder.More() {
		if err := inspectJSONObjectField(decoder, fields, seen); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func skipTypedJSONObject(valueType reflect.Type) bool {
	return valueType == nil || valueType == rawMessageType || valueType == timeType || valueType.Kind() != reflect.Struct
}

func inspectJSONObjectField(decoder *json.Decoder, fields map[string]reflect.Type, seen map[string]struct{}) error {
	name, err := readJSONObjectFieldName(decoder)
	if err != nil {
		return err
	}
	fieldType, ok := fields[name]
	if !ok {
		return fmt.Errorf("field %q is not a canonical field name", name)
	}
	if _, exists := seen[name]; exists {
		return fmt.Errorf("duplicate object field %q", name)
	}
	seen[name] = struct{}{}
	return inspectJSONValueWithType(decoder, fieldType)
}

func readJSONObjectFieldName(decoder *json.Decoder) (string, error) {
	nameToken, err := decoder.Token()
	if err != nil {
		return "", err
	}
	name, ok := nameToken.(string)
	if !ok {
		return "", fmt.Errorf("object field name is not a string")
	}
	return name, nil
}

func inspectJSONArrayFields(decoder *json.Decoder, valueType reflect.Type) error {
	valueType = indirectJSONType(valueType)
	itemType := reflect.TypeFor[any]()
	if valueType != nil && (valueType.Kind() == reflect.Array || valueType.Kind() == reflect.Slice) {
		itemType = valueType.Elem()
	}
	for decoder.More() {
		if err := inspectJSONValueWithType(decoder, itemType); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func consumeJSONContainer(decoder *json.Decoder, opening json.Delim) error {
	closing := json.Delim('}')
	if opening == '[' {
		closing = ']'
	}
	for decoder.More() {
		if err := inspectJSONValueWithType(decoder, nil); err != nil {
			return err
		}
	}
	if token, err := decoder.Token(); err != nil {
		return err
	} else if token != closing {
		return fmt.Errorf("unexpected delimiter %q", token)
	}
	return nil
}

func indirectJSONType(valueType reflect.Type) reflect.Type {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

func jsonFieldTypes(valueType reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, valueType.NumField())
	for field := range valueType.Fields() {
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

func validateJSONText(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("decode json: invalid UTF-8")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		end, err := validateJSONString(raw, i+1)
		if err != nil {
			return err
		}
		i = end
	}
	return nil
}

func validateJSONString(raw []byte, start int) (int, error) {
	for i := start; i < len(raw); i++ {
		end, done, err := validateJSONStringByte(raw, i)
		if err != nil || done {
			return end, err
		}
		i = end
	}
	return 0, fmt.Errorf("decode json: unterminated string")
}

func validateJSONStringByte(raw []byte, index int) (next int, escaped bool, err error) {
	if raw[index] == '"' {
		return index, true, nil
	}
	if raw[index] == '\\' {
		end, err := validateJSONEscape(raw, index)
		return end, false, err
	}
	if raw[index] < 0x20 {
		return 0, true, fmt.Errorf("decode json: invalid control character in string")
	}
	return index, false, nil
}

func validateJSONEscape(raw []byte, start int) (int, error) {
	if start+1 >= len(raw) {
		return 0, fmt.Errorf("decode json: unterminated escape sequence")
	}
	if raw[start+1] != 'u' {
		return validateSimpleJSONEscape(raw[start+1], start)
	}
	return validateJSONUnicodeEscape(raw, start)
}

func validateSimpleJSONEscape(marker byte, start int) (int, error) {
	if strings.IndexByte(`"\/bfnrt`, marker) >= 0 {
		return start + 1, nil
	}
	return 0, fmt.Errorf("decode json: invalid escape sequence")
}

func validateJSONUnicodeEscape(raw []byte, start int) (int, error) {
	if start+6 > len(raw) {
		return 0, fmt.Errorf("decode json: incomplete Unicode escape")
	}
	codeUnit, ok := parseJSONHex4(raw[start+2 : start+6])
	if !ok {
		return 0, fmt.Errorf("decode json: invalid Unicode escape")
	}
	if codeUnit >= 0xD800 && codeUnit <= 0xDBFF {
		return validateJSONLowSurrogate(raw, start)
	}
	if codeUnit >= 0xDC00 && codeUnit <= 0xDFFF {
		return 0, fmt.Errorf("decode json: low surrogate is not preceded by a high surrogate")
	}
	return start + 5, nil
}

func validateJSONLowSurrogate(raw []byte, highStart int) (int, error) {
	lowStart := highStart + 6
	if lowStart+6 > len(raw) || raw[lowStart] != '\\' || raw[lowStart+1] != 'u' {
		return 0, fmt.Errorf("decode json: high surrogate is not followed by a low surrogate")
	}
	low, ok := parseJSONHex4(raw[lowStart+2 : lowStart+6])
	if !ok || low < 0xDC00 || low > 0xDFFF {
		return 0, fmt.Errorf("decode json: high surrogate is not followed by a low surrogate")
	}
	return lowStart + 5, nil
}

func parseJSONHex4(raw []byte) (uint16, bool) {
	if len(raw) != 4 {
		return 0, false
	}
	var value uint16
	for _, digit := range raw {
		nibble, ok := parseJSONHexDigit(digit)
		if !ok {
			return 0, false
		}
		value = value<<4 + nibble
	}
	return value, true
}

func parseJSONHexDigit(digit byte) (uint16, bool) {
	if digit >= '0' && digit <= '9' {
		return uint16(digit - '0'), true
	}
	if digit >= 'a' && digit <= 'f' {
		return uint16(digit-'a') + 10, true
	}
	if digit >= 'A' && digit <= 'F' {
		return uint16(digit-'A') + 10, true
	}
	return 0, false
}

func inspectJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	return inspectJSONContainer(decoder, delimiter, depth)
}

func inspectJSONContainer(decoder *json.Decoder, delimiter json.Delim, depth int) error {
	if depth >= MaxCanonicalJSONDepth {
		return fmt.Errorf("json nesting exceeds %d", MaxCanonicalJSONDepth)
	}
	if delimiter == '{' {
		return inspectJSONNames(decoder, depth+1)
	}
	if delimiter == '[' {
		return inspectJSONArray(decoder, depth+1)
	}
	return fmt.Errorf("unexpected delimiter %q", delimiter)
}

func inspectJSONNames(decoder *json.Decoder, depth int) error {
	seen := make(map[string]struct{})
	for decoder.More() {
		if err := inspectJSONName(decoder, seen, depth); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func inspectJSONName(decoder *json.Decoder, seen map[string]struct{}, depth int) error {
	name, err := readJSONObjectFieldName(decoder)
	if err != nil {
		return err
	}
	if _, exists := seen[name]; exists {
		return fmt.Errorf("duplicate object field %q", name)
	}
	seen[name] = struct{}{}
	return inspectJSONValue(decoder, depth)
}

func inspectJSONArray(decoder *json.Decoder, depth int) error {
	for decoder.More() {
		if err := inspectJSONValue(decoder, depth); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}

func canonicalJSON(value any) ([]byte, error) {
	if err := validateCanonicalJSONStrings(value); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return CanonicalizeJSON(encoded)
}
