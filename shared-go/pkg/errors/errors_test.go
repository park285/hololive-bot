package errors

import (
	"errors"
	"testing"
)

func TestRedisError(t *testing.T) {
	origErr := errors.New("connection refused")
	redisErr := NewRedisError("GET", "user:123", origErr)

	expected := "redis GET [user:123]: connection refused"
	if redisErr.Error() != expected {
		t.Errorf("expected %q, got %q", expected, redisErr.Error())
	}

	if !errors.Is(redisErr, origErr) {
		t.Error("Unwrap should return original error")
	}
}

func TestRedisError_NoKey(t *testing.T) {
	origErr := errors.New("timeout")
	redisErr := NewRedisError("PING", "", origErr)

	expected := "redis PING: timeout"
	if redisErr.Error() != expected {
		t.Errorf("expected %q, got %q", expected, redisErr.Error())
	}
}

func TestDatabaseError(t *testing.T) {
	origErr := errors.New("duplicate key")
	dbErr := NewDatabaseError("insert", "users", origErr)

	expected := "database insert [users]: duplicate key"
	if dbErr.Error() != expected {
		t.Errorf("expected %q, got %q", expected, dbErr.Error())
	}

	if !errors.Is(dbErr, origErr) {
		t.Error("Unwrap should return original error")
	}
}

func TestDatabaseError_NoTable(t *testing.T) {
	origErr := errors.New("connection failed")
	dbErr := NewDatabaseError("connect", "", origErr)

	expected := "database connect: connection failed"
	if dbErr.Error() != expected {
		t.Errorf("expected %q, got %q", expected, dbErr.Error())
	}
}

func TestExternalAPIError(t *testing.T) {
	origErr := errors.New("rate limit exceeded")
	apiErr := NewExternalAPIError("gemini", 429, origErr)

	expected := "external api gemini [429]: rate limit exceeded"
	if apiErr.Error() != expected {
		t.Errorf("expected %q, got %q", expected, apiErr.Error())
	}

	if !errors.Is(apiErr, origErr) {
		t.Error("Unwrap should return original error")
	}
}

func TestExternalAPIError_NoStatusCode(t *testing.T) {
	origErr := errors.New("network error")
	apiErr := NewExternalAPIError("kakao", 0, origErr)

	expected := "external api kakao: network error"
	if apiErr.Error() != expected {
		t.Errorf("expected %q, got %q", expected, apiErr.Error())
	}
}

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrNotFound,
		ErrAlreadyExists,
		ErrInvalidInput,
		ErrUnauthorized,
		ErrForbidden,
		ErrTimeout,
		ErrRateLimited,
		ErrServiceDown,
	}

	for _, sentinel := range sentinels {
		if sentinel == nil {
			t.Error("sentinel error should not be nil")
		}
	}
}
