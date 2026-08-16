package collecterr

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

const (
	Timeout         = "collection_timeout"
	Canceled        = "collection_canceled"
	ParserDrift     = "parser_drift"
	PaginationGap   = "pagination_gap"
	Cooldown        = "cooldown"
	LeaseLost       = "lease_lost"
	PublishRejected = "publish_rejected"
	Failed          = "collection_failed"
	AcquireFailed   = "lease_acquire_failed"
	NotAcquired     = "lease_not_acquired"
	DeferFailed     = "lease_defer_failed"
	CandidateFailed = "candidate_load_failed"
	UnknownClass    = "unknown_error"
	MaxDetailBytes  = 2048
)

var errorClassPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

type Error struct {
	Code    string
	Class   string
	retryAt time.Time
	err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return e.Code
	}
	return e.err.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func Wrap(code string, err error) error {
	return WrapClass(code, UnknownClass, err)
}

func WrapClass(code, class string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Class: normalizeClass(class), err: err}
}

func New(code, message string) error {
	return &Error{Code: code, err: fmt.Errorf("%s", message)}
}

func CooldownUntil(message string, retryAt time.Time) error {
	return &Error{Code: Cooldown, retryAt: retryAt.UTC(), err: fmt.Errorf("%s", message)}
}

func RetryAt(err error) (time.Time, bool) {
	var typed *Error
	if errors.As(err, &typed) && typed != nil && !typed.retryAt.IsZero() {
		return typed.retryAt, true
	}
	return time.Time{}, false
}

func Code(err error) string {
	var typed *Error
	if errors.As(err, &typed) && typed != nil && typed.Code != "" {
		return typed.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout
	}
	if errors.Is(err, context.Canceled) {
		return Canceled
	}
	return Failed
}

func Class(err error) string {
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return normalizeClass(typed.Class)
	}
	return UnknownClass
}

func Detail(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeDetail(err.Error())
}

func SanitizeDetail(detail string) string {
	detail = strings.ToValidUTF8(detail, "�")
	detail = sharedlogging.RedactDiagnostic(detail)
	if len(detail) <= MaxDetailBytes {
		return detail
	}
	cut := MaxDetailBytes
	for cut > 0 && !utf8.RuneStart(detail[cut]) {
		cut--
	}
	return detail[:cut]
}

func normalizeClass(class string) string {
	if errorClassPattern.MatchString(class) {
		return class
	}
	return UnknownClass
}

func FromContext(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Wrap(Timeout, err)
	}
	if errors.Is(err, context.Canceled) {
		return Wrap(Canceled, err)
	}
	return Wrap(Failed, err)
}
