package template

import (
	"fmt"
	"reflect"
)

const (
	// TemplateNumberSize는 정수 하나의 fmt 출력 상한입니다(%#b의 MinInt64가 부호·접두사 포함 67바이트).
	templateNumberSize = 68
	// TemplateFloatSize는 실수 하나의 fmt 출력 상한입니다(%f의 MaxFloat64가 309자리 + 소수 6자리 + 부호).
	// 명시한 precision은 형식 검사에서 따로 더합니다.
	templateFloatSize = 330
	// TemplatePointerSize는 주소·chan·func 출력(0x와 16진수 16자리)의 상한입니다.
	templatePointerSize = 20
	// TemplateNilSize는 "<nil>" 표기 길이입니다.
	templateNilSize = len("<nil>")
	// TemplateMethodPanicSize는 String/Error 메서드 panic을 fmt가 "%!v(PANIC=...)"로 적을 때의 여유분입니다.
	templateMethodPanicSize = 256
	// TemplateMeasureCeiling은 인자 하나를 재는 상한입니다. 이보다 큰 값은 64KiB 출력 상한 안에서 쓸 수 없으므로
	// 끝까지 순회하지 않고 거절합니다.
	templateMeasureCeiling = 16 << 20
)

// valueMeasure는 fmt가 값을 출력할 때 만들 수 있는 바이트 수의 상한과 width·precision이 각각 적용되는
// leaf 출력 수를 셉니다. 모든 방문 노드는 1바이트 이상을 더하므로 순회 비용도 limit으로 제한됩니다.
type valueMeasure struct {
	size, leaves, limit int
}

// measureTemplateValue는 v의 fmt 출력 크기 상한과 leaf 수를 돌려줍니다. 상한이 limit을 넘으면 ok가 false입니다.
// 문자열 길이, 숫자 최대 표기, 컨테이너 구분자·타입 이름, String/Error 메서드 결과를 더하며
// %v·%s 출력의 상한이며, %#v의 문자열 escape나 잘못된 verb 표기는 이 값의 상수배(최대 약 4배)까지 커질 수 있습니다.
// 그런 초과분은 각 함수의 결과 크기 검사가 상수배 할당 뒤 거절합니다.
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
		// fmt는 reflect.Value 인자를 그 안의 값으로 출력합니다.
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
		// walk가 먼저 처리하며 여기에는 오지 않습니다. 값 없음 표기로 셉니다.
		return m.leaf(templateNilSize)
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return m.leaf(templatePointerSize)
	default:
		return m.leaf(templatePointerSize)
	}
}

// fmt는 최상위 포인터가 가리키는 컨테이너만 &{...}로 펼치고 그 아래 포인터는 주소로 출력합니다.
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
		// []byte는 %v에서 원소마다 숫자와 공백(최대 4바이트)으로, %s·%x에서는 더 짧게 출력됩니다.
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

// methodOutputSize는 fmt가 호출할 Formatter·error·Stringer 메서드의 출력 길이를 잽니다. 메서드는 호출자 데이터가
// 제공하며 template은 이런 값을 만들 수 없습니다. 숫자 verb로 출력될 수도 있어 숫자 상한을 더합니다.
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

// fmt는 메서드 panic을 출력 문자열로 바꾸므로 측정도 실패 대신 panic 표기 상한을 씁니다.
func safeMethodLen(method func() string) (size int) {
	defer func() {
		if recover() != nil {
			size = templateMethodPanicSize
		}
	}()

	return len(method())
}
