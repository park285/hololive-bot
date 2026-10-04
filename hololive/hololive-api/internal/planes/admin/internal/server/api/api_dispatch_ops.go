package api

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/dispatchops"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

const (
	dispatchOpsTimeout = 5 * time.Second
	dispatchOpsMaxBody = 64 << 10
)

// DispatchOperations는 관리자 API가 사용하는 원장 조회 및 재처리 계약입니다.
// 구현은 재처리와 감사 기록의 원자성 및 결과 불명 요청의 비재실행을 보장해야 합니다.
//
//nolint:interfacebloat // 원장 HTTP 계약의 7개 작업을 한 의존성으로 연결하므로 메서드 수 경고는 오탐입니다.
type DispatchOperations interface {
	Summary(context.Context) (dispatchops.Summary, error)
	Failures(context.Context) (dispatchops.FailureBreakdown, error)
	List(context.Context, dispatchops.Filter) (dispatchops.Page, error)
	Detail(context.Context, string) (dispatchops.Detail, error)
	Actions(context.Context, string, string) (dispatchops.ActionPage, error)
	Settle(context.Context, string, dispatchops.SettleRequest) (dispatchops.RequeueResult, error)
	Requeue(context.Context, string, dispatchops.RequeueRequest) (dispatchops.RequeueResult, error)
}

// SetDispatchOperations는 라우트 등록 전 생성 단계에서만 운영 저장소를 연결합니다.
// 요청 처리 중 교체하거나 nil 의존성을 성공 응답으로 대체하지 않습니다.
func (h *AlarmHandler) SetDispatchOperations(ops DispatchOperations) { h.dispatchOps = ops }

func (h *AlarmHandler) dispatchReady(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")

	if h == nil || h.Handler == nil || h.dispatchOps == nil {
		respondServiceUnavailable(c, "dispatch operations unavailable")

		return false
	}

	return true
}

func dispatchQuery(c *gin.Context, allowed ...string) (url.Values, bool) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		sharedserver.RespondError(c, 400, "invalid dispatch query", nil)

		return nil, false
	}

	keys := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		keys[key] = true
	}

	for key, values := range query {
		if !keys[key] || len(values) != 1 || values[0] == "" {
			sharedserver.RespondError(c, 400, "invalid dispatch query", nil)

			return nil, false
		}
	}

	return query, true
}

func dispatchError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "dispatch operation failed; refresh the delivery and audit history before another attempt"

	switch {
	case errors.Is(err, dispatchops.ErrInvalidInput):
		status, message = 400, "invalid dispatch operation input"
	case errors.Is(err, dispatchops.ErrNotFound):
		status, message = 404, "dispatch delivery not found"
	case errors.Is(err, dispatchops.ErrConflict):
		status, message = 409, "delivery or send unit changed; refresh before another attempt"
	case errors.Is(err, dispatchops.ErrUnavailable):
		status, message = 503, "dispatch operations unavailable"
	}

	// DB 에러, SQL, 본문 및 운영 사유를 응답이나 로그에 그대로 노출하지 않습니다.
	sharedserver.RespondError(c, status, message, nil)
}

// GetDispatchSummary는 보존 중인 전체 원장의 상태별 건수와 가장 오래된 생성 시각을 조회합니다.
func (h *AlarmHandler) GetDispatchSummary(c *gin.Context) {
	if !h.dispatchReady(c) {
		return
	}

	if _, ok := dispatchQuery(c); !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.Summary(ctx)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}

// GetDispatchFailures는 실패 보관·격리 원장의 상위 원인 분포를 조회합니다.
func (h *AlarmHandler) GetDispatchFailures(c *gin.Context) {
	if !h.dispatchReady(c) {
		return
	}

	if _, ok := dispatchQuery(c); !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.Failures(ctx)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}

// GetDispatchDeliveries는 상태·채팅방·채널·알림 유형·오류 코드와 ID 커서로 최대 50건을 조회합니다.
func (h *AlarmHandler) GetDispatchDeliveries(c *gin.Context) {
	if !h.dispatchReady(c) {
		return
	}

	query, ok := dispatchQuery(c, "status", "roomId", "channelId", "beforeId", "alarmType", "errorCode")
	if !ok {
		return
	}

	filter := dispatchops.Filter{
		Status: query.Get("status"), RoomID: query.Get("roomId"), ChannelID: query.Get("channelId"),
		BeforeID: query.Get("beforeId"), AlarmType: query.Get("alarmType"), ErrorCode: query.Get("errorCode"),
	}
	if err := filter.Validate(); err != nil {
		dispatchError(c, err)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.List(ctx, filter)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}

// GetDispatchDelivery는 본문 없이 상태와 재처리 묶음 전체의 리비전을 반환합니다.
func (h *AlarmHandler) GetDispatchDelivery(c *gin.Context) {
	if !h.dispatchReady(c) {
		return
	}

	if _, ok := dispatchQuery(c); !ok {
		return
	}

	id := c.Param("id")
	if _, err := dispatchops.ParseID(id); err != nil {
		dispatchError(c, err)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.Detail(ctx, id)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}

// GetDispatchActions는 해당 발송 항목의 감사 이력을 최대 50건 반환합니다.
func (h *AlarmHandler) GetDispatchActions(c *gin.Context) {
	if !h.dispatchReady(c) {
		return
	}

	query, ok := dispatchQuery(c, "beforeId")
	if !ok {
		return
	}

	id := c.Param("id")
	if _, err := dispatchops.ParseID(id); err != nil {
		dispatchError(c, err)

		return
	}

	before := query.Get("beforeId")
	if before != "" {
		if _, err := dispatchops.ParseID(before); err != nil {
			dispatchError(c, err)

			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.Actions(ctx, id, before)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}

// decodeDispatchMutation은 변경 입력의 크기·필드·감사 정보·리비전을 실행 전에 검증합니다.
func (h *AlarmHandler) decodeDispatchMutation(c *gin.Context, request interface{ Validate(string) error }) bool {
	if !h.dispatchReady(c) {
		return false
	}

	if _, ok := dispatchQuery(c); !ok {
		return false
	}

	id := c.Param("id")
	if _, err := dispatchops.ParseID(id); err != nil {
		dispatchError(c, err)

		return false
	}

	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		sharedserver.RespondError(c, 415, "application/json required", nil)

		return false
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, dispatchOpsMaxBody)

	if decodeErr := jsonv2.UnmarshalRead(c.Request.Body, request, jsonv2.RejectUnknownMembers(true)); decodeErr != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](decodeErr); ok {
			sharedserver.RespondError(c, 413, "dispatch request body too large", nil)
		} else {
			sharedserver.RespondError(c, 400, "invalid dispatch request body", nil)
		}

		return false
	}

	if validationErr := request.Validate(id); validationErr != nil {
		dispatchError(c, validationErr)

		return false
	}

	return true
}

// RequeueDispatchDelivery는 명시적인 중복 위험 확인과 운영 사유를 받아 묶음 전체를 재처리합니다.
func (h *AlarmHandler) RequeueDispatchDelivery(c *gin.Context) {
	var request dispatchops.RequeueRequest

	if !h.decodeDispatchMutation(c, &request) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.Requeue(ctx, c.Param("id"), request)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}

// SettleDispatchDelivery는 사유와 리비전을 검증한 뒤 외부 발송 없이 실패 묶음을 처리합니다.
func (h *AlarmHandler) SettleDispatchDelivery(c *gin.Context) {
	var request dispatchops.SettleRequest

	if !h.decodeDispatchMutation(c, &request) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dispatchOpsTimeout)

	defer cancel()

	result, err := h.dispatchOps.Settle(ctx, c.Param("id"), request)
	if err != nil {
		dispatchError(c, err)

		return
	}

	ginjson.Respond(c, 200, result)
}
