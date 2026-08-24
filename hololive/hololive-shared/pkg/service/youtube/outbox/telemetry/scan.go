package telemetry

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type scannableTime struct {
	value *time.Time
}

func (s *scannableTime) Scan(value any) error {
	if err := s.scanValue(value); err != nil {
		return fmt.Errorf("scan time value: %w", err)
	}

	return nil
}

func (s *scannableTime) scanValue(value any) error {
	if value == nil {
		s.value = nil

		return nil
	}

	if v, ok := value.(time.Time); ok {
		s.value = normalizedTimePtr(v)

		return nil
	}

	raw, ok := scanRawString(value)
	if !ok {
		return fmt.Errorf("scan time: unsupported type %T", value)
	}

	if err := s.scanString(raw); err != nil {
		return fmt.Errorf("parse scannable time: %w", err)
	}

	return nil
}

func (s *scannableTime) scanString(raw string) error {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		s.value = nil

		return nil
	}

	parsed, ok := parseScannableTime(cleaned)
	if !ok {
		return fmt.Errorf("scan time: unsupported value %q", cleaned)
	}

	s.value = parsed

	return nil
}

func scanRawString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}

//nolint:nilnil // driver.Valuer 계약상 SQL NULL은 (nil, nil)로만 표현할 수 있다.
func (s *scannableTime) Value() (driver.Value, error) {
	if s.value == nil {
		return nil, nil
	}

	return s.value.UTC(), nil
}

func (s *scannableTime) Ptr() *time.Time {
	if s.value == nil {
		return nil
	}

	normalized := s.value.UTC()

	return &normalized
}

func (s *scannableTime) Require(field string) (time.Time, error) {
	if s.value == nil {
		return time.Time{}, fmt.Errorf("%s is empty", field)
	}

	return s.value.UTC(), nil
}

type scannableBool struct {
	value *bool
}

func (s *scannableBool) Scan(value any) error {
	if err := s.scanValue(value); err != nil {
		return fmt.Errorf("scan bool value: %w", err)
	}

	return nil
}

func (s *scannableBool) scanValue(value any) error {
	if value == nil {
		s.value = nil

		return nil
	}

	if parsed, ok := scanBoolPrimitive(value); ok {
		s.value = parsed

		return nil
	}

	raw, ok := scanRawString(value)
	if !ok {
		return fmt.Errorf("scan bool: unsupported type %T", value)
	}

	if err := s.scanString(raw); err != nil {
		return fmt.Errorf("scan bool string: %w", err)
	}

	return nil
}

func (s *scannableBool) scanString(raw string) error {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		s.value = nil

		return nil
	}

	parsed, ok := parseScannableBool(cleaned)
	if !ok {
		return fmt.Errorf("scan bool: unsupported value %q", cleaned)
	}

	s.value = parsed

	return nil
}

func scanBoolPrimitive(value any) (*bool, bool) {
	if v, ok := value.(bool); ok {
		return new(v), true
	}

	return scanBoolInteger(value)
}

func scanBoolInteger(value any) (*bool, bool) {
	switch v := value.(type) {
	case int64:
		return new(v != 0), true
	case int32:
		return new(v != 0), true
	case int:
		return new(v != 0), true
	default:
		return nil, false
	}
}

func (s *scannableBool) Ptr() *bool {
	if s.value == nil {
		return nil
	}

	value := *s.value

	return &value
}

//nolint:nilnil // driver.Valuer 계약상 SQL NULL은 (nil, nil)로만 표현할 수 있다.
func (s *scannableBool) Value() (driver.Value, error) {
	if s.value == nil {
		return nil, nil
	}

	return *s.value, nil
}

func parseScannableBool(cleaned string) (*bool, bool) {
	if parsed, err := strconv.ParseBool(cleaned); err == nil {
		return new(parsed), true
	}

	switch cleaned {
	case "0":
		return new(false), true
	case "1":
		return new(true), true
	default:
		return nil, false
	}
}

func parseScannableTime(cleaned string) (*time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
	} {
		parsed, err := time.Parse(layout, cleaned)
		if err == nil {
			return normalizedTimePtr(parsed), true
		}
	}

	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		time.DateTime,
	} {
		parsed, err := time.ParseInLocation(layout, cleaned, time.UTC)
		if err == nil {
			return normalizedTimePtr(parsed), true
		}
	}

	return nil, false
}

func normalizedTimePtr(value time.Time) *time.Time {
	normalized := value.UTC()

	return &normalized
}
