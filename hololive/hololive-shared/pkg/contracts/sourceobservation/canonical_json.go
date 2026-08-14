package sourceobservation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
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

func CanonicalizeJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("payload is empty")
	}
	if len(raw) > MaxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds %d bytes", MaxPayloadBytes)
	}
	if err := validateJSONText(raw); err != nil {
		return nil, err
	}
	if err := rejectDuplicateJSONNames(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
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

func appendCanonicalJSON(destination []byte, value any, depth int) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return append(destination, "null"...), nil
	case bool:
		if typed {
			return append(destination, "true"...), nil
		}
		return append(destination, "false"...), nil
	case string:
		return appendCanonicalJSONString(destination, typed), nil
	case json.Number:
		canonical, err := canonicalizeIntegerJSONNumber(string(typed))
		if err != nil {
			return nil, err
		}
		return append(destination, canonical...), nil
	case []any:
		if depth >= MaxCanonicalJSONDepth {
			return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
		}
		destination = append(destination, '[')
		for index := range typed {
			if index > 0 {
				destination = append(destination, ',')
			}
			var err error
			destination, err = appendCanonicalJSON(destination, typed[index], depth+1)
			if err != nil {
				return nil, err
			}
		}
		return append(destination, ']'), nil
	case map[string]any:
		if depth >= MaxCanonicalJSONDepth {
			return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
		}
		members := make([]canonicalJSONMember, 0, len(typed))
		for name, memberValue := range typed {
			members = append(members, canonicalJSONMember{
				name: name, order: utf16.Encode([]rune(name)), value: memberValue,
			})
		}
		sort.Slice(members, func(i, j int) bool {
			return lessUTF16(members[i].order, members[j].order)
		})
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
	default:
		return nil, fmt.Errorf("canonical json contains unsupported value type")
	}
}

func appendCanonicalJSONString(destination []byte, value string) []byte {
	const hexadecimal = "0123456789abcdef"
	destination = append(destination, '"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			destination = append(destination, '\\', byte(character))
		case '\b':
			destination = append(destination, '\\', 'b')
		case '\t':
			destination = append(destination, '\\', 't')
		case '\n':
			destination = append(destination, '\\', 'n')
		case '\f':
			destination = append(destination, '\\', 'f')
		case '\r':
			destination = append(destination, '\\', 'r')
		default:
			if character < 0x20 {
				destination = append(
					destination,
					'\\', 'u', '0', '0',
					hexadecimal[byte(character)>>4],
					hexadecimal[byte(character)&0x0f],
				)
				continue
			}
			destination = utf8.AppendRune(destination, character)
		}
	}
	return append(destination, '"')
}

func lessUTF16(left, right []uint16) bool {
	limit := min(len(left), len(right))
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return len(left) < len(right)
}

func canonicalizeIntegerJSONNumber(raw string) (string, error) {
	negative := strings.HasPrefix(raw, "-")
	unsigned := strings.TrimPrefix(raw, "-")
	exponentText := ""
	if exponentIndex := strings.IndexAny(unsigned, "eE"); exponentIndex >= 0 {
		exponentText = unsigned[exponentIndex+1:]
		unsigned = unsigned[:exponentIndex]
	}
	integerPart := unsigned
	fractionalPart := ""
	if decimalIndex := strings.IndexByte(unsigned, '.'); decimalIndex >= 0 {
		integerPart = unsigned[:decimalIndex]
		fractionalPart = unsigned[decimalIndex+1:]
	}
	digits := strings.TrimLeft(integerPart+fractionalPart, "0")
	if digits == "" {
		return "0", nil
	}

	var exponent int64
	if exponentText != "" {
		parsed, err := strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return "", fmt.Errorf("canonical json number exponent is outside the accepted range")
		}
		exponent = parsed
	}
	scale := exponent - int64(len(fractionalPart))
	if scale < 0 {
		fractionalDigits := -scale
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
	} else {
		if int64(len(digits))+scale > int64(len(maxCanonicalSafeIntegerString)) {
			return "", fmt.Errorf("canonical json integer exceeds the safe range")
		}
		digits += strings.Repeat("0", int(scale))
	}
	if len(digits) > len(maxCanonicalSafeIntegerString) ||
		len(digits) == len(maxCanonicalSafeIntegerString) && digits > maxCanonicalSafeIntegerString {
		return "", fmt.Errorf("canonical json integer exceeds the safe range")
	}
	if negative {
		return "-" + digits, nil
	}
	return digits, nil
}

func validateCanonicalJSONStrings(value any) error {
	return validateCanonicalJSONStringValue(reflect.ValueOf(value), 0)
}

func validateCanonicalJSONStringValue(value reflect.Value, depth int) error {
	if !value.IsValid() {
		return nil
	}
	if depth > MaxCanonicalJSONDepth {
		return fmt.Errorf("canonical json value nesting exceeds %d", MaxCanonicalJSONDepth)
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return validateCanonicalJSONStringValue(value.Elem(), depth+1)
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return fmt.Errorf("canonical json string contains invalid UTF-8")
		}
	case reflect.Struct:
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath != "" || strings.Split(field.Tag.Get("json"), ",")[0] == "-" {
				continue
			}
			if err := validateCanonicalJSONStringValue(value.Field(index), depth+1); err != nil {
				return err
			}
		}
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateCanonicalJSONStringValue(iterator.Key(), depth+1); err != nil {
				return err
			}
			if err := validateCanonicalJSONStringValue(iterator.Value(), depth+1); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		for index := 0; index < value.Len(); index++ {
			if err := validateCanonicalJSONStringValue(value.Index(index), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
