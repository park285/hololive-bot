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

package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIError_ErrorAndUnwrap(t *testing.T) {
	base := APIError{Operation: "op", StatusCode: 500}
	if got := base.Error(); got != "api error operation=op status=500" {
		t.Fatalf("unexpected APIError string: %q", got)
	}

	cause := errors.New("network down")
	withCause := APIError{Operation: "op", StatusCode: 503, Err: cause}
	if got := withCause.Error(); !strings.Contains(got, "network down") {
		t.Fatalf("expected cause in APIError string: %q", got)
	}
	if !errors.Is(withCause.Unwrap(), cause) {
		t.Fatalf("APIError.Unwrap() mismatch")
	}
}

func TestNewAPIError_OperationSelection(t *testing.T) {
	err := NewAPIError("fallback-op", 429, map[string]any{"operation": "explicit-op"})
	if err.Operation != "explicit-op" || err.StatusCode != 429 {
		t.Fatalf("unexpected NewAPIError result: %+v", err)
	}

	err = NewAPIError("fallback-op", 400, map[string]any{"operation": 123})
	if err.Operation != "fallback-op" {
		t.Fatalf("non-string operation should fallback to message, got=%q", err.Operation)
	}
}

func TestKeyRotationError_ErrorAndFactory(t *testing.T) {
	base := KeyRotationError{Operation: "url-a", StatusCode: 503}
	if got := base.Error(); got != "key rotation exhausted operation=url-a status=503" {
		t.Fatalf("unexpected KeyRotationError string: %q", got)
	}

	err := NewKeyRotationError("fallback", 429, map[string]any{"url": "https://api.example.com"})
	if err.Operation != "https://api.example.com" || err.StatusCode != 429 {
		t.Fatalf("unexpected NewKeyRotationError result: %+v", err)
	}

	err = NewKeyRotationError("fallback", 429, map[string]any{"url": 123})
	if err.Operation != "fallback" {
		t.Fatalf("non-string url should fallback to message, got=%q", err.Operation)
	}
}

func TestCacheError_ErrorAndFactory(t *testing.T) {
	base := CacheError{Operation: "get", Key: "k1"}
	if got := base.Error(); got != "cache error operation=get key=k1" {
		t.Fatalf("unexpected CacheError string: %q", got)
	}

	cause := errors.New("redis timeout")
	withCause := CacheError{Operation: "set", Key: "k2", Err: cause}
	if got := withCause.Error(); !strings.Contains(got, "redis timeout") {
		t.Fatalf("expected cause in CacheError string: %q", got)
	}
	if !errors.Is(withCause.Unwrap(), cause) {
		t.Fatalf("CacheError.Unwrap() mismatch")
	}

	created := NewCacheError("ignored", "delete", "k3", cause)
	if created.Operation != "delete" || created.Key != "k3" || !errors.Is(created.Err, cause) {
		t.Fatalf("unexpected NewCacheError result: %+v", created)
	}
}

func TestCircuitOpenError_Error(t *testing.T) {
	err := CircuitOpenError{RetryAfterMs: 1500}
	if got := err.Error(); got != "circuit breaker open retry_after_ms=1500" {
		t.Fatalf("unexpected CircuitOpenError string: %q", got)
	}
}

func TestValidationError_ErrorAndFactory(t *testing.T) {
	withoutField := ValidationError{Message: "invalid input"}
	if got := withoutField.Error(); got != "invalid input" {
		t.Fatalf("unexpected ValidationError(without field): %q", got)
	}

	withField := ValidationError{Field: "email", Message: "required"}
	if got := withField.Error(); got != "validation error field=email: required" {
		t.Fatalf("unexpected ValidationError(with field): %q", got)
	}

	created := NewValidationError("too short", "password", "x")
	if created.Field != "password" || created.Message != "too short" {
		t.Fatalf("unexpected NewValidationError result: %+v", created)
	}
}

func TestServiceError_ErrorAndFactory(t *testing.T) {
	base := ServiceError{Service: "member", Operation: "sync"}
	if got := base.Error(); got != "service error service=member operation=sync" {
		t.Fatalf("unexpected ServiceError string: %q", got)
	}

	cause := errors.New("db failed")
	withCause := ServiceError{Service: "member", Operation: "sync", Err: cause}
	if got := withCause.Error(); !strings.Contains(got, "db failed") {
		t.Fatalf("expected cause in ServiceError string: %q", got)
	}
	if !errors.Is(withCause.Unwrap(), cause) {
		t.Fatalf("ServiceError.Unwrap() mismatch")
	}

	created := NewServiceError("ignored", "alarm", "publish", cause)
	if created.Service != "alarm" || created.Operation != "publish" || !errors.Is(created.Err, cause) {
		t.Fatalf("unexpected NewServiceError result: %+v", created)
	}
}
