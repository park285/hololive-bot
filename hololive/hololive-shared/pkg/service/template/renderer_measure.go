package template

import (
	"fmt"
	"reflect"
)

const (
	templateNumberSize      = 68
	templateFloatSize       = 330
	templatePointerSize     = 20
	templateNilSize         = len("<nil>")
	templateMethodPanicSize = 256
	templateMeasureCeiling  = 16 << 20
)

type valueMeasure struct {
	size, leaves, limit int
}

func measureTemplateValue(v any, limit int) (size, leaves int, ok bool) {
	if s, isString := v.(string); isString {
		return len(s), 1, len(s) <= limit
	}

	m := valueMeasure{limit: limit}

	ok = m.walk(reflect.ValueOf(v), 0)

	return m.size, m.leaves, ok
}

func (m *valueMeasure) add(n int) bool {
	m.size += n

	return m.size <= m.limit
}

func (m *valueMeasure) leaf(n int) bool {
	m.leaves++

	return m.add(n)
}

func (m *valueMeasure) walk(v reflect.Value, depth int) bool {
	if !v.IsValid() {
		return m.leaf(templateNilSize)
	}

	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return m.leaf(templateNilSize)
		}

		return m.walk(v.Elem(), depth)
	}

	if v.CanInterface() {
		if inner, ok := reflect.TypeAssert[reflect.Value](v); ok {
			return m.walk(inner, depth)
		}
	}

	if size, ok := methodOutputSize(v); ok {
		return m.leaf(size)
	}

	return m.walkKind(v, depth)
}

func (m *valueMeasure) walkKind(v reflect.Value, depth int) bool {
	switch v.Kind() {
	case reflect.Bool:
		return m.leaf(len("false"))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return m.leaf(templateNumberSize)
	case reflect.Float32, reflect.Float64:
		return m.leaf(templateFloatSize)
	case reflect.Complex64, reflect.Complex128:
		return m.leaf(2*templateFloatSize + len("(i)"))
	case reflect.String:
		return m.leaf(v.Len())
	case reflect.Pointer:
		return m.walkPointer(v, depth)
	case reflect.Slice, reflect.Array:
		return m.walkList(v, depth)
	case reflect.Map:
		return m.walkMap(v, depth)
	case reflect.Struct:
		return m.walkStruct(v, depth)
	case reflect.Invalid, reflect.Interface:
		return m.leaf(templateNilSize)
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return m.leaf(templatePointerSize)
	default:
		return m.leaf(templatePointerSize)
	}
}

func (m *valueMeasure) walkPointer(v reflect.Value, depth int) bool {
	if v.IsNil() {
		return m.leaf(templateNilSize)
	}

	if depth == 0 && isExpandedPointerTarget(v.Elem().Kind()) {
		return m.add(1) && m.walk(v.Elem(), depth+1)
	}

	return m.leaf(templatePointerSize)
}

func isExpandedPointerTarget(kind reflect.Kind) bool {
	return kind == reflect.Array || kind == reflect.Slice || kind == reflect.Struct || kind == reflect.Map
}

func (m *valueMeasure) walkList(v reflect.Value, depth int) bool {
	if !m.add(2 + len(v.Type().String())) {
		return false
	}

	if v.Type().Elem().Kind() == reflect.Uint8 {
		m.leaves += v.Len()

		return m.add(4 * v.Len())
	}

	for i := range v.Len() {
		if !m.add(1) || !m.walk(v.Index(i), depth+1) {
			return false
		}
	}

	return true
}

func (m *valueMeasure) walkMap(v reflect.Value, depth int) bool {
	if !m.add(len("map[]") + len(v.Type().String())) {
		return false
	}

	iter := v.MapRange()
	for iter.Next() {
		if !m.add(2) || !m.walk(iter.Key(), depth+1) || !m.walk(iter.Value(), depth+1) {
			return false
		}
	}

	return true
}

func (m *valueMeasure) walkStruct(v reflect.Value, depth int) bool {
	if !m.add(2 + len(v.Type().String())) {
		return false
	}

	for i := range v.NumField() {
		if !m.add(len(v.Type().Field(i).Name)+2) || !m.walk(v.Field(i), depth+1) {
			return false
		}
	}

	return true
}

func methodOutputSize(v reflect.Value) (size int, ok bool) {
	if !v.CanInterface() {
		return 0, false
	}

	switch value := v.Interface().(type) {
	case fmt.Formatter:
		return safeMethodLen(func() string { return fmt.Sprint(value) }) + templateNumberSize, true
	case error:
		return safeMethodLen(value.Error) + templateNumberSize, true
	case fmt.Stringer:
		return safeMethodLen(value.String) + templateNumberSize, true
	default:
		return 0, false
	}
}

func safeMethodLen(method func() string) (size int) {
	defer func() {
		if recover() != nil {
			size = templateMethodPanicSize
		}
	}()

	return len(method())
}
