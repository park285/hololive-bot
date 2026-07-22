package httpx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/ginjson"
	"github.com/park285/shared-go/pkg/json"
)

type ErrorResponse struct {
	Error           string  `json:"error"`
	Code            string  `json:"code,omitempty"`
	Details         any     `json:"details,omitempty"`
	AbsoluteExpired *bool   `json:"absolute_expired,omitempty"`
	RetryAfter      *uint64 `json:"retry_after,omitempty"`
}

type AppError struct {
	Status int
	Body   ErrorResponse
	Cause  error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Body.Error
}

func (e *AppError) Unwrap() error { return e.Cause }

func NewError(status int, message string) *AppError {
	return &AppError{Status: status, Body: ErrorResponse{Error: message}}
}

func Unauthorized() *AppError { return NewError(http.StatusUnauthorized, "Unauthorized") }
func Forbidden() *AppError    { return NewError(http.StatusForbidden, "Forbidden") }
func BadGateway() *AppError   { return NewError(http.StatusBadGateway, "Service unavailable") }
func StoreUnavailable() *AppError {
	return NewError(http.StatusServiceUnavailable, "Session store unavailable")
}

func BadRequest(message string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Body: ErrorResponse{Error: message, Code: "bad_request"}}
}

func Internal(err error) *AppError {
	return &AppError{Status: http.StatusInternalServerError, Body: ErrorResponse{Error: "An internal error occurred"}, Cause: err}
}

func JSON(w http.ResponseWriter, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(payload)
}

func Error(w http.ResponseWriter, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		respondJSON(w, appErr.Status, appErr.Body)
		return
	}
	respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "An internal error occurred"})
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	if err := JSON(w, status, payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func Abort(c *gin.Context, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		ginjson.Respond(c, appErr.Status, appErr.Body)
		c.Abort()
		return
	}
	ginjson.Respond(c, http.StatusInternalServerError, ErrorResponse{Error: "An internal error occurred"})
	c.Abort()
}

func DecodeJSON(r *http.Request, dst any, maxBytes int64) error {
	defer closeBody(r.Body)
	if maxBytes <= 0 {
		return fmt.Errorf("invalid json body: maximum body size must be positive")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("invalid json body: read body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("invalid json body: body exceeds %d bytes", maxBytes)
	}
	return DecodeJSONBytes(body, dst)
}

// DecodeJSONBytes decodes exactly one JSON value and rejects unknown fields.
func DecodeJSONBytes(body []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("invalid json body: trailing data: %w", err)
		}
		return fmt.Errorf("invalid json body: multiple json values are not allowed")
	}
	return nil
}

func closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		return
	}
}
