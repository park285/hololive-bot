package httpx

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"
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
	body, err := jsonv2.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write response body: %w", err)
	}

	return nil
}

func Error(w http.ResponseWriter, err error) {
	if appErr, ok := errors.AsType[*AppError](err); ok {
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
	if appErr, ok := errors.AsType[*AppError](err); ok {
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
		return errors.New("invalid json body: maximum body size must be positive")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("invalid json body: read body: %w", err)
	}

	if int64(len(body)) > maxBytes {
		return fmt.Errorf("invalid json body: body exceeds %d bytes", maxBytes)
	}

	if err := DecodeJSONBytes(body, dst); err != nil {
		return fmt.Errorf("decode JSON bytes: %w", err)
	}

	return nil
}

// DecodeJSONBytes decodes exactly one JSON value and rejects unknown fields.
func DecodeJSONBytes(body []byte, dst any) error {
	if err := jsonv2.Unmarshal(body, dst, jsonv2.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}

	return nil
}

func closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		return
	}
}
