package outbox

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
	parsed, err := scanTimeValue(value)
	if err != nil {
		return err
	}
	s.value = parsed
	return nil
}

func scanTimeValue(value any) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if v, ok := value.(time.Time); ok {
		return normalizedTimePtr(v), nil
	}
	if raw, ok := scanRawString(value); ok {
		return parseScannableTime(raw)
	}
	return nil, fmt.Errorf("scan time: unsupported type %T", value)
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

func (s scannableTime) Value() (driver.Value, error) {
	if s.value == nil {
		return nil, nil
	}
	return s.value.UTC(), nil
}

func (s scannableTime) Ptr() *time.Time {
	if s.value == nil {
		return nil
	}
	normalized := s.value.UTC()
	return &normalized
}

func (s scannableTime) Require(field string) (time.Time, error) {
	if s.value == nil {
		return time.Time{}, fmt.Errorf("%s is empty", field)
	}
	return s.value.UTC(), nil
}

type scannableBool struct {
	value *bool
}

func (s *scannableBool) Scan(value any) error {
	parsed, err := scanBoolValue(value)
	if err != nil {
		return err
	}
	s.value = parsed
	return nil
}

func scanBoolValue(value any) (*bool, error) {
	if value == nil {
		return nil, nil
	}
	if parsed, ok := scanBoolPrimitive(value); ok {
		return parsed, nil
	}
	if raw, ok := scanRawString(value); ok {
		return scanBoolString(raw)
	}
	return nil, fmt.Errorf("scan bool: unsupported type %T", value)
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

func (s scannableBool) Ptr() *bool {
	if s.value == nil {
		return nil
	}
	value := *s.value
	return &value
}

func (s scannableBool) Value() (driver.Value, error) {
	if s.value == nil {
		return nil, nil
	}
	return *s.value, nil
}

func scanBoolString(raw string) (*bool, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return nil, nil
	}

	if parsed, err := strconv.ParseBool(cleaned); err == nil {
		return new(parsed), nil
	}

	switch cleaned {
	case "0":
		return new(false), nil
	case "1":
		return new(true), nil
	default:
		return nil, fmt.Errorf("scan bool: unsupported value %q", cleaned)
	}
}

func parseScannableTime(raw string) (*time.Time, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return nil, nil
	}

	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
	} {
		parsed, err := time.Parse(layout, cleaned)
		if err == nil {
			normalized := parsed.UTC()
			return &normalized, nil
		}
	}

	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		parsed, err := time.ParseInLocation(layout, cleaned, time.UTC)
		if err == nil {
			normalized := parsed.UTC()
			return &normalized, nil
		}
	}

	return nil, fmt.Errorf("scan time: unsupported value %q", cleaned)
}

func normalizedTimePtr(value time.Time) *time.Time {
	normalized := value.UTC()
	return &normalized
}
