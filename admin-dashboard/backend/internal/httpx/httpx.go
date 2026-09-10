package httpx

import (
	"context"
	"crypto/rand"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
)

// Error는 Gin 밖의 HTTP 경계에서도 같은 요청 ID와 안전한 오류 envelope를 사용합니다.
func Error(w http.ResponseWriter, request *http.Request, err error) {
	requestID, ok := request.Context().Value(requestIDContextKey{}).(string)
	if !ok || requestID == "" {
		requestID = rand.Text()
	}

	if appErr, ok := errors.AsType[*contract.AppError](err); ok {
		respondJSON(w, appErr.Status, errorBody(request.Context(), appErr.Status, appErr.Body, requestID))

		return
	}

	respondJSON(w, http.StatusInternalServerError, errorBody(request.Context(), http.StatusInternalServerError, contract.ErrorResponse{Error: "An internal error occurred"}, requestID))
}

func respondJSON(w http.ResponseWriter, status int, payload contract.ErrorResponse) {
	response := jsonResponse{data: payload}
	response.WriteContentType(w)
	w.WriteHeader(status)

	// 고정 DTO를 관리자 JSON renderer로 쓰며 I/O 실패 후 다른 응답을 덧붙이지 않습니다.
	if err := response.Render(w); err != nil {
		return
	}
}

func Abort(c *gin.Context, err error) {
	if appErr, ok := errors.AsType[*contract.AppError](err); ok {
		Respond(c, appErr.Status, errorBody(c.Request.Context(), appErr.Status, appErr.Body, RequestID(c)))
		c.Abort()

		return
	}

	Respond(c, http.StatusInternalServerError, errorBody(c.Request.Context(), http.StatusInternalServerError, contract.ErrorResponse{Error: "An internal error occurred"}, RequestID(c)))
	c.Abort()
}

// RequestID는 요청별 서버 생성 감사 식별자를 한 번 발급하여 오류 응답과 공유합니다.
func RequestID(c *gin.Context) string {
	const key = "admin-request-id"

	if id := c.GetString(key); id != "" {
		return id
	}

	id := rand.Text()
	c.Set(key, id)

	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), requestIDContextKey{}, id))

	return id
}

type requestIDContextKey struct{}

func errorBody(ctx context.Context, status int, body contract.ErrorResponse, requestID string) contract.ErrorResponse {
	body.RequestID = requestID
	// 원문 오류가 제공한 근거를 신뢰하지 않고 최초 claim/dispatch context만 사용합니다.
	body.NotDispatchedMutationID = contract.NotDispatchedMutationID(ctx)
	if body.Code != "" {
		return body
	}

	codes := map[int]string{
		http.StatusBadRequest: "BAD_REQUEST", http.StatusUnauthorized: "UNAUTHORIZED",
		http.StatusForbidden: "FORBIDDEN", http.StatusNotFound: "NOT_FOUND",
		http.StatusMethodNotAllowed: "METHOD_NOT_ALLOWED", http.StatusConflict: "CONFLICT",
		http.StatusRequestEntityTooLarge: "PAYLOAD_TOO_LARGE", http.StatusTooManyRequests: "RATE_LIMITED",
		http.StatusBadGateway: "UPSTREAM_UNAVAILABLE", http.StatusServiceUnavailable: "SERVICE_UNAVAILABLE",
	}

	if code, ok := codes[status]; ok {
		body.Code = code
	} else {
		body.Code = "INTERNAL_ERROR"
	}

	return body
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
