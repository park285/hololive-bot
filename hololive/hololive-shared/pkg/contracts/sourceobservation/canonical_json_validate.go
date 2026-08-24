package sourceobservation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

func validateCanonicalJSONStrings(value any) error {
	if err := validateCanonicalJSONStringValue(reflect.ValueOf(value), 0); err != nil {
		return fmt.Errorf("validate canonical JSON string value: %w", err)
	}

	return nil
}

func validateCanonicalJSONStringValue(value reflect.Value, depth int) error {
	if !value.IsValid() {
		return nil
	}

	if depth > MaxCanonicalJSONDepth {
		return fmt.Errorf("canonical json value nesting exceeds %d", MaxCanonicalJSONDepth)
	}

	if err := validateCanonicalJSONKind(value, depth); err != nil {
		return fmt.Errorf("validate canonical JSON kind: %w", err)
	}

	return nil
}

func validateCanonicalJSONKind(value reflect.Value, depth int) error {
	kind := value.Kind()
	if kind == reflect.Interface || kind == reflect.Pointer {
		return errors.Join(validateCanonicalJSONIndirectKind(value, depth))
	}

	if kind == reflect.String {
		return errors.Join(validateCanonicalJSONStringValueKind(value))
	}

	if kind == reflect.Struct {
		return errors.Join(validateCanonicalJSONStructKind(value, depth))
	}

	if err := validateCanonicalJSONCollection(value, depth); err != nil {
		return fmt.Errorf("validate canonical JSON collection: %w", err)
	}

	return nil
}

func validateCanonicalJSONIndirectKind(value reflect.Value, depth int) error {
	if err := validateCanonicalJSONIndirect(value, depth); err != nil {
		return fmt.Errorf("validate canonical JSON indirect: %w", err)
	}

	return nil
}

func validateCanonicalJSONStringValueKind(value reflect.Value) error {
	if err := validateCanonicalJSONStringKind(value); err != nil {
		return fmt.Errorf("validate canonical JSON string kind: %w", err)
	}

	return nil
}

func validateCanonicalJSONStructKind(value reflect.Value, depth int) error {
	if err := validateCanonicalJSONStruct(value, depth); err != nil {
		return fmt.Errorf("validate canonical JSON struct: %w", err)
	}

	return nil
}

func validateCanonicalJSONCollection(value reflect.Value, depth int) error {
	kind := value.Kind()
	if kind == reflect.Map {
		if err := validateCanonicalJSONMap(value, depth); err != nil {
			return fmt.Errorf("validate canonical JSON map: %w", err)
		}

		return nil
	}

	if kind == reflect.Array || kind == reflect.Slice {
		if err := validateCanonicalJSONList(value, depth); err != nil {
			return fmt.Errorf("validate canonical JSON list: %w", err)
		}

		return nil
	}

	return nil
}

func validateCanonicalJSONIndirect(value reflect.Value, depth int) error {
	if value.IsNil() {
		return nil
	}

	if err := validateCanonicalJSONStringValue(value.Elem(), depth+1); err != nil {
		return fmt.Errorf("validate canonical JSON string value: %w", err)
	}

	return nil
}

func validateCanonicalJSONStringKind(value reflect.Value) error {
	if !utf8.ValidString(value.String()) {
		return errors.New("canonical json string contains invalid UTF-8")
	}

	return nil
}

func validateCanonicalJSONStruct(value reflect.Value, depth int) error {
	valueType := value.Type()
	for index := range value.NumField() {
		field := valueType.Field(index)
		if field.PkgPath != "" || strings.Split(field.Tag.Get("json"), ",")[0] == "-" {
			continue
		}

		if err := validateCanonicalJSONStringValue(value.Field(index), depth+1); err != nil {
			return fmt.Errorf("validate canonical JSON string value: %w", err)
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
			return fmt.Errorf("validate canonical JSON string value: %w", err)
		}

		if err := validateCanonicalJSONStringValue(iterator.Value(), depth+1); err != nil {
			return fmt.Errorf("validate canonical JSON string value: %w", err)
		}
	}

	return nil
}

func validateCanonicalJSONList(value reflect.Value, depth int) error {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return nil
	}

	for index := range value.Len() {
		if err := validateCanonicalJSONStringValue(value.Index(index), depth+1); err != nil {
			return fmt.Errorf("validate canonical JSON string value: %w", err)
		}
	}

	return nil
}
