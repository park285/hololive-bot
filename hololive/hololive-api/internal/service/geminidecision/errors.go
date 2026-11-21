package geminidecision

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

var errRequestFailed = errors.New("gemini request failed")

type transportError struct {
	error

	timedOut bool
}

func (e transportError) Timeout() bool { return e.timedOut }

func (e transportError) Unwrap() error { return e.error }

func SafeClientError(err error) error {
	cause := errRequestFailed

	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		cause = errno
	}

	for _, known := range []error{context.Canceled, context.DeadlineExceeded, httputil.ErrResponseBodyTooLarge, io.ErrUnexpectedEOF, io.EOF} {
		if errors.Is(err, known) {
			cause = known
			break
		}
	}

	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return &url.Error{Op: http.MethodPost, URL: baseURL, Err: transportError{error: cause, timedOut: urlErr.Timeout()}}
	}

	return cause
}

type StatusError struct {
	HTTPStatus int
	safetyCode string
	status     string
	reason     string
}

func (e *StatusError) Error() string {
	parts := []string{fmt.Sprintf("decision status %d", e.HTTPStatus)}

	for _, known := range []string{e.safetyCode, e.status, e.reason} {
		if known != "" {
			parts = append(parts, known)
		}
	}

	return strings.Join(parts, " ")
}

func (e *StatusError) SafetyBlocked() bool {
	return e.safetyCode != ""
}

func (e *StatusError) ConfigurationFailure() bool {
	return e.status != "" || e.reason != ""
}

func failure(status int, raw []byte) error {
	err := &StatusError{HTTPStatus: status}

	switch status {
	case http.StatusBadRequest:
		return err.badRequest(raw)
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	default:
		return err
	}
}

type errorMessage struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (e *StatusError) badRequest(raw []byte) error {
	var decision struct {
		Error struct {
			errorMessage

			Code any `json:"code"`
		} `json:"error"`
	}

	var rpc struct {
		Error struct {
			errorMessage

			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}

	decisionErr := jsonv2.Unmarshal(raw, &decision)
	rpcErr := jsonv2.Unmarshal(raw, &rpc)

	if code, ok := decision.Error.Code.(string); ok && decisionErr == nil && slices.Contains([]string{
		"safety", "prohibited_content", "blocklist", "image_safety", "content_blocked",
	}, code) {
		e.safetyCode = code
	}

	if rpc.Error.Status == "FAILED_PRECONDITION" {
		e.status = rpc.Error.Status
	}

	for _, detail := range rpc.Error.Details {
		if detail.Reason == "API_KEY_INVALID" {
			e.reason = detail.Reason
			break
		}
	}

	switch {
	case decisionErr != nil || rpcErr != nil:
		return fmt.Errorf("%w: %w", ErrRejected, e)
	case e.SafetyBlocked():
		return fmt.Errorf("%w: %w", ErrBlocked, e)
	case e.ConfigurationFailure():
		return fmt.Errorf("%w: %w", ErrUnavailable, e)
	default:
		return fmt.Errorf("%w: %w", ErrRejected, e)
	}
}
