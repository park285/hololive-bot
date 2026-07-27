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

package chzzk

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func reflectStringField(v any, name string) string {
	field, ok := reflectStructField(v, name)
	if !ok {
		return ""
	}
	return reflectStringValue(field)
}

func reflectStringValue(field reflect.Value) string {
	kind := field.Kind()
	if kind == reflect.String {
		return strings.TrimSpace(field.String())
	}
	return reflectIntegerString(field)
}

func reflectIntegerString(field reflect.Value) string {
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflectSignedIntegerString(field)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflectUnsignedIntegerString(field)
	case reflect.Invalid,
		reflect.Bool,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice,
		reflect.String,
		reflect.Struct,
		reflect.UnsafePointer:
		return ""
	default:
		return ""
	}
}

func reflectSignedIntegerString(field reflect.Value) string {
	if field.Int() == 0 {
		return ""
	}
	return strconv.FormatInt(field.Int(), 10)
}

func reflectUnsignedIntegerString(field reflect.Value) string {
	if field.Uint() == 0 {
		return ""
	}
	return strconv.FormatUint(field.Uint(), 10)
}

func reflectTimeField(v any, name string) time.Time {
	field, ok := reflectStructField(v, name)
	if !ok {
		return time.Time{}
	}
	return reflectTimeValue(field)
}

func reflectStructField(v any, name string) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return reflect.Value{}, false
	}

	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	field := elem.FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}, false
	}
	return field, true
}

func reflectTimeValue(field reflect.Value) time.Time {
	if field.Type() == reflect.TypeFor[time.Time]() {
		parsed, ok := field.Interface().(time.Time)
		if !ok {
			return time.Time{}
		}
		return parsed
	}

	if field.Kind() == reflect.String {
		return parseChzzkTime(field.String())
	}

	if parsed := reflectSignedUnixTime(field); !parsed.IsZero() {
		return parsed
	}
	return reflectUnsignedUnixTime(field)
}

func reflectSignedUnixTime(field reflect.Value) time.Time {
	if field.Kind() < reflect.Int || field.Kind() > reflect.Int64 {
		return time.Time{}
	}
	raw := field.Int()
	if raw <= 0 {
		return time.Time{}
	}
	return unixByMagnitude(raw)
}

func reflectUnsignedUnixTime(field reflect.Value) time.Time {
	if field.Kind() < reflect.Uint || field.Kind() > reflect.Uint64 {
		return time.Time{}
	}
	raw := field.Uint()
	if raw <= 0 {
		return time.Time{}
	}
	if raw > uint64(math.MaxInt64) {
		return time.Time{}
	}
	return unixByMagnitude(int64(raw))
}

func parseChzzkTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}

	if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
		return unixByMagnitude(n)
	}

	return time.Time{}
}

func unixByMagnitude(raw int64) time.Time {
	switch {
	case raw > 1_000_000_000_000_000:
		return time.UnixMicro(raw)
	case raw > 1_000_000_000_000:
		return time.UnixMilli(raw)
	default:
		return time.Unix(raw, 0)
	}
}
