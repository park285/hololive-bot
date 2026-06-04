package httpx

import (
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

func (e AppError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Body.Error
}

func (e AppError) Unwrap() error { return e.Cause }

func NewError(status int, message string) AppError {
	return AppError{Status: status, Body: ErrorResponse{Error: message}}
}

func Unauthorized() AppError { return NewError(http.StatusUnauthorized, "Unauthorized") }
func Forbidden() AppError    { return NewError(http.StatusForbidden, "Forbidden") }
func BadGateway() AppError   { return NewError(http.StatusBadGateway, "Service unavailable") }
func StoreUnavailable() AppError {
	return NewError(http.StatusServiceUnavailable, "Session store unavailable")
}

func BadRequest(message string) AppError {
	return AppError{Status: http.StatusBadRequest, Body: ErrorResponse{Error: message, Code: "bad_request"}}
}

func Internal(err error) AppError {
	return AppError{Status: http.StatusInternalServerError, Body: ErrorResponse{Error: "An internal error occurred"}, Cause: err}
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func Error(w http.ResponseWriter, err error) {
	var appErr AppError
	if errors.As(err, &appErr) {
		JSON(w, appErr.Status, appErr.Body)
		return
	}
	JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "An internal error occurred"})
}

func Abort(c *gin.Context, err error) {
	var appErr AppError
	if errors.As(err, &appErr) {
		ginjson.Respond(c, appErr.Status, appErr.Body)
		c.Abort()
		return
	}
	ginjson.Respond(c, http.StatusInternalServerError, ErrorResponse{Error: "An internal error occurred"})
	c.Abort()
}

func DecodeJSON(r *http.Request, dst any, maxBytes int64) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}
	return nil
}
