package sourceobservation

import (
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

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
	return validateCanonicalJSONKind(value, depth)
}

func validateCanonicalJSONKind(value reflect.Value, depth int) error {
	kind := value.Kind()
	if kind == reflect.Interface || kind == reflect.Pointer {
		return validateCanonicalJSONIndirect(value, depth)
	}
	if kind == reflect.String {
		return validateCanonicalJSONStringKind(value)
	}
	if kind == reflect.Struct {
		return validateCanonicalJSONStruct(value, depth)
	}
	return validateCanonicalJSONCollection(value, depth)
}

func validateCanonicalJSONCollection(value reflect.Value, depth int) error {
	kind := value.Kind()
	if kind == reflect.Map {
		return validateCanonicalJSONMap(value, depth)
	}
	if kind == reflect.Array || kind == reflect.Slice {
		return validateCanonicalJSONList(value, depth)
	}
	return nil
}

func validateCanonicalJSONIndirect(value reflect.Value, depth int) error {
	if value.IsNil() {
		return nil
	}
	return validateCanonicalJSONStringValue(value.Elem(), depth+1)
}

func validateCanonicalJSONStringKind(value reflect.Value) error {
	if !utf8.ValidString(value.String()) {
		return fmt.Errorf("canonical json string contains invalid UTF-8")
	}
	return nil
}

func validateCanonicalJSONStruct(value reflect.Value, depth int) error {
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
	return nil
}

func validateCanonicalJSONMap(value reflect.Value, depth int) error {
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
	return nil
}

func validateCanonicalJSONList(value reflect.Value, depth int) error {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return nil
	}
	for index := 0; index < value.Len(); index++ {
		if err := validateCanonicalJSONStringValue(value.Index(index), depth+1); err != nil {
			return err
		}
	}
	return nil
}
