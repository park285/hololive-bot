package sourceobservation

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strconv"
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
		return nil, errors.New("payload is empty")
	}

	if len(raw) > MaxPayloadBytes {
		return nil, fmt.Errorf("payload exceeds %d bytes", MaxPayloadBytes)
	}

	decoder := jsontext.NewDecoder(bytes.NewReader(raw))

	value, err := readCanonicalJSONValue(decoder, 0)
	if err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}

	if _, readErr := decoder.ReadToken(); !errors.Is(readErr, io.EOF) {
		if readErr == nil {
			return nil, errors.New("decode json: trailing value")
		}

		return nil, fmt.Errorf("decode json trailing data: %w", readErr)
	}

	canonical, err := appendCanonicalJSON(make([]byte, 0, len(raw)), value, 0)
	if err != nil {
		return nil, fmt.Errorf("append canonical JSON: %w", err)
	}

	if len(canonical) > MaxPayloadBytes {
		return nil, fmt.Errorf("canonical payload exceeds %d bytes", MaxPayloadBytes)
	}

	return canonical, nil
}

func readCanonicalJSONValue(decoder *jsontext.Decoder, depth int) (any, error) {
	token, err := decoder.ReadToken()
	if err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}

	kind := token.Kind()
	if kind == jsontext.KindBeginArray {
		values, arrayErr := readCanonicalJSONArray(decoder, depth)
		if arrayErr != nil {
			return nil, fmt.Errorf("read canonical JSON array: %w", arrayErr)
		}

		return values, nil
	}

	if kind == jsontext.KindBeginObject {
		members, objectErr := readCanonicalJSONObject(decoder, depth)
		if objectErr != nil {
			return nil, fmt.Errorf("read canonical JSON object: %w", objectErr)
		}

		return members, nil
	}

	scalar, ok := canonicalJSONScalar(token)
	if !ok {
		return nil, fmt.Errorf("canonical JSON scalar: unexpected json token %q", kind)
	}

	return scalar, nil
}

func canonicalJSONScalar(token jsontext.Token) (any, bool) {
	kind := token.Kind()
	if kind == jsontext.KindNull {
		return nil, true
	}

	if kind == jsontext.KindTrue || kind == jsontext.KindFalse {
		return token.Bool(), true
	}

	if kind == jsontext.KindString {
		return token.String(), true
	}

	if kind == jsontext.KindNumber {
		return canonicalJSONNumber(token.String()), true
	}

	return nil, false
}

func readCanonicalJSONArray(decoder *jsontext.Decoder, depth int) ([]any, error) {
	if depth >= MaxCanonicalJSONDepth {
		return nil, fmt.Errorf("canonical json nesting exceeds %d", MaxCanonicalJSONDepth)
	}

	values := make([]any, 0)

	for decoder.PeekKind() != jsontext.KindEndArray {
		value, err := readCanonicalJSONValue(decoder, depth+1)
		if err != nil {
			return nil, fmt.Errorf("read canonical JSON value: %w", err)
		}

		values = append(values, value)
	}

	if _, err := decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("read token: %w", err)
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
			return nil, fmt.Errorf("read canonical JSON field: %w", err)
		}

		values[name] = value
	}

	if _, err := decoder.ReadToken(); err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}

	return values, nil
}

func readCanonicalJSONField(decoder *jsontext.Decoder, depth int) (name string, value any, err error) {
	nameToken, err := decoder.ReadToken()
	if err != nil {
		return "", nil, fmt.Errorf("read token: %w", err)
	}

	if nameToken.Kind() != jsontext.KindString {
		return "", nil, errors.New("object field name is not a string")
	}

	name = nameToken.String()

	value, err = readCanonicalJSONValue(decoder, depth+1)
	if err != nil {
		return "", nil, fmt.Errorf("read canonical JSON value: %w", err)
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
		out, err := appendCanonicalJSONOther(destination, value, depth)

		return out, errors.Join(err)
	}
}

func appendCanonicalJSONOther(destination []byte, value any, depth int) ([]byte, error) {
	out, err := appendCanonicalJSONNumberOrComposite(destination, value, depth)
	if err != nil {
		return out, fmt.Errorf("append canonical JSON number or composite: %w", err)
	}

	return out, nil
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
		out, err := appendCanonicalNumberValue(destination, typed)

		return out, errors.Join(err)
	case []any:
		out, err := appendCanonicalArrayValue(destination, typed, depth)

		return out, errors.Join(err)
	case map[string]any:
		out, err := appendCanonicalObjectValue(destination, typed, depth)

		return out, errors.Join(err)
	default:
		return nil, errors.New("canonical json contains unsupported value type")
	}
}

func appendCanonicalNumberValue(destination []byte, value canonicalJSONNumber) ([]byte, error) {
	out, err := appendCanonicalJSONNumber(destination, value)
	if err != nil {
		return out, fmt.Errorf("append canonical JSON number: %w", err)
	}

	return out, nil
}

func appendCanonicalArrayValue(destination []byte, value []any, depth int) ([]byte, error) {
	out, err := appendCanonicalJSONArray(destination, value, depth)
	if err != nil {
		return out, fmt.Errorf("append canonical JSON array: %w", err)
	}

	return out, nil
}

func appendCanonicalObjectValue(destination []byte, value map[string]any, depth int) ([]byte, error) {
	out, err := appendCanonicalJSONObject(destination, value, depth)
	if err != nil {
		return out, fmt.Errorf("append canonical JSON object: %w", err)
	}

	return out, nil
}

func appendCanonicalJSONNumber(destination []byte, value canonicalJSONNumber) ([]byte, error) {
	canonical, err := canonicalizeIntegerJSONNumber(string(value))
	if err != nil {
		return nil, fmt.Errorf("canonicalize integer JSON number: %w", err)
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
			return nil, fmt.Errorf("append canonical JSON: %w", err)
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
			return nil, fmt.Errorf("append canonical JSON: %w", err)
		}
	}

	return append(destination, '}'), nil
}
