package api

import (
	"context"
	"encoding/base64"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

var (
	errInvalidSchedulePosterRequest = errors.New("invalid schedule poster request body")
	schedulePosterDatePattern       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

const (
	schedulePosterTimeout = 5 * time.Second
	schedulePosterMaxBody = 4 << 10
	schedulePosterListMax = 50
)

// SchedulePosterCollector는 수집을 수집 주기와 상관없이 바로 돌리게 예약하며, 이미 예약돼 있으면 false다.
type SchedulePosterCollector interface {
	RequestCollect() bool
}

type SchedulePosterHandler struct {
	store     *scheduleposter.Store
	collector SchedulePosterCollector
	logger    *slog.Logger
}

type schedulePosterResponse struct {
	ID                string  `json:"id"`
	ChannelID         string  `json:"channelId"`
	PostURL           string  `json:"postUrl"`
	ImageURL          string  `json:"imageUrl"`
	PostedAt          string  `json:"postedAt"`
	TextExcerpt       string  `json:"textExcerpt"`
	ReviewStatus      string  `json:"reviewStatus"`
	WeekStart         *string `json:"weekStart"`
	WeekEnd           *string `json:"weekEnd"`
	ReviewedAt        *string `json:"reviewedAt"`
	ReviewedBy        string  `json:"reviewedBy"`
	ReviewReason      string  `json:"reviewRequestReason"`
	TranslationStatus string  `json:"translationStatus"`
	TranslationError  string  `json:"translationError"`
	TranslatedAt      *string `json:"translatedAt"`
	Revision          string  `json:"revision"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`
}

type schedulePosterRevisionRequest struct {
	ExpectedRevision string `json:"expectedRevision"`
}

type schedulePosterWeekRequest struct {
	ExpectedRevision string `json:"expectedRevision"`
	WeekStart        string `json:"weekStart"`
	WeekEnd          string `json:"weekEnd"`
}

func newSchedulePosterResponse(poster *scheduleposter.Poster) schedulePosterResponse {
	return schedulePosterResponse{
		ID:                strconv.FormatInt(poster.ID, 10),
		ChannelID:         poster.ChannelID,
		PostURL:           poster.PostURL,
		ImageURL:          poster.ImageURL,
		PostedAt:          formatTimestamp(poster.PostedAt),
		TextExcerpt:       poster.TextExcerpt,
		ReviewStatus:      string(poster.ReviewStatus),
		WeekStart:         formatOptionalDate(poster.WeekStart),
		WeekEnd:           formatOptionalDate(poster.WeekEnd),
		ReviewedAt:        formatOptionalTimestamp(poster.ReviewedAt),
		ReviewedBy:        string(poster.ReviewedBy),
		ReviewReason:      string(poster.ReviewRequestReason),
		TranslationStatus: string(poster.TranslationStatus),
		TranslationError:  poster.TranslationError,
		TranslatedAt:      formatOptionalTimestamp(poster.TranslatedAt),
		Revision:          strconv.FormatInt(poster.Revision, 10),
		CreatedAt:         formatTimestamp(poster.CreatedAt),
		UpdatedAt:         formatTimestamp(poster.UpdatedAt),
	}
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}

func formatOptionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}

	return new(formatTimestamp(*value))
}

func formatOptionalDate(value *time.Time) *string {
	if value == nil {
		return nil
	}

	return new(value.Format(time.DateOnly))
}

func (h *SchedulePosterHandler) ready(c *gin.Context) bool {
	if h == nil || h.store == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "schedule posters unavailable", nil)

		return false
	}

	return true
}

func (h *SchedulePosterHandler) posterID(c *gin.Context) (int64, bool) {
	raw := c.Param("id")

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster id", nil)

		return 0, false
	}

	return id, true
}

func (h *SchedulePosterHandler) respondStoreError(c *gin.Context, action string, err error) {
	switch {
	case errors.Is(err, errInvalidSchedulePosterRequest):
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)
	case errors.Is(err, scheduleposter.ErrNotFound):
		sharedserver.RespondError(c, http.StatusNotFound, "schedule poster not found", nil)
	case errors.Is(err, scheduleposter.ErrRevisionConflict):
		sharedserver.RespondError(c, http.StatusConflict, "schedule poster changed; refresh before retrying", gin.H{"code": "revision_conflict"})
	case errors.Is(err, scheduleposter.ErrInvalidState):
		sharedserver.RespondError(c, http.StatusConflict, "schedule poster state does not allow this change", gin.H{"code": "invalid_state"})
	case errors.Is(err, scheduleposter.ErrInvalidWeek):
		sharedserver.RespondError(c, http.StatusConflict, "week must span at most 7 days within 7 days of the post", gin.H{"code": "invalid_week"})
	default:
		sharedserver.RespondInternalError(h.logger, c, "schedule poster operation failed", action, err)
	}
}

// GetSchedulePosterSummary는 Console push monitor가 새 후보 알림을 판단하는 값이다.
func (h *SchedulePosterHandler) GetSchedulePosterSummary(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	summary, err := h.store.Summary(ctx)
	if err != nil {
		h.respondStoreError(c, "load schedule poster summary", err)

		return
	}

	latest := ""

	if summary.LatestCandidateID > 0 {
		latest = strconv.FormatInt(summary.LatestCandidateID, 10)
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"pendingCount": summary.PendingCount, "latestCandidateId": latest})
}

func (h *SchedulePosterHandler) GetSchedulePosters(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	status := scheduleposter.ReviewStatus(query.Get("status"))

	if err != nil || len(query) != 1 || len(query["status"]) != 1 || !status.Valid() {
		sharedserver.RespondError(c, http.StatusBadRequest, "status must be one review status", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	posters, err := h.store.List(ctx, status, schedulePosterListMax)
	if err != nil {
		h.respondStoreError(c, "list schedule posters", err)

		return
	}

	items := make([]schedulePosterResponse, 0, len(posters))
	for i := range posters {
		items = append(items, newSchedulePosterResponse(&posters[i]))
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"posters": items})
}

func (h *SchedulePosterHandler) GetSchedulePoster(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	poster, err := h.store.Get(ctx, id)
	if err != nil {
		h.respondStoreError(c, "get schedule poster", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, newSchedulePosterResponse(&poster))
}

// GetTranslatedSchedulePoster는 Console gateway가 JSON 응답만 중계하므로 이미지를 base64로 담는다.
func (h *SchedulePosterHandler) GetTranslatedSchedulePoster(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	image, err := h.store.TranslatedImage(ctx, id)
	if err != nil {
		h.respondStoreError(c, "load translated schedule poster", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"id": strconv.FormatInt(id, 10), "mimeType": image.MIME, "data": base64.StdEncoding.EncodeToString(image.Data)})
}

func (h *SchedulePosterHandler) ConfirmSchedulePoster(c *gin.Context) {
	var request schedulePosterWeekRequest

	h.change(c, &request, &request.ExpectedRevision, func(ctx context.Context, id, revision int64) (int64, error) {
		weekStart, weekEnd, err := request.week()
		if err != nil {
			return 0, err
		}

		return h.store.Confirm(ctx, id, revision, weekStart, weekEnd)
	})
}

// UpdateSchedulePosterWeek은 확정본의 주 범위만 고치고 다시 번역하지 않는다.
func (h *SchedulePosterHandler) UpdateSchedulePosterWeek(c *gin.Context) {
	var request schedulePosterWeekRequest

	h.change(c, &request, &request.ExpectedRevision, func(ctx context.Context, id, revision int64) (int64, error) {
		weekStart, weekEnd, err := request.week()
		if err != nil {
			return 0, err
		}

		return h.store.UpdateWeek(ctx, id, revision, weekStart, weekEnd)
	})
}

func (r *schedulePosterWeekRequest) week() (time.Time, time.Time, error) {
	if !schedulePosterDatePattern.MatchString(r.WeekStart) || !schedulePosterDatePattern.MatchString(r.WeekEnd) {
		return time.Time{}, time.Time{}, errInvalidSchedulePosterRequest
	}

	weekStart, startErr := time.Parse(time.DateOnly, r.WeekStart)
	weekEnd, endErr := time.Parse(time.DateOnly, r.WeekEnd)

	if startErr != nil || endErr != nil {
		return time.Time{}, time.Time{}, scheduleposter.ErrInvalidWeek
	}

	return weekStart, weekEnd, nil
}

// CollectSchedulePosters는 수집을 예약만 하고 끝나기를 기다리지 않는다. 결과는 목록과 요약에 나타난다.
// 다른 변경과 같이 JSON 본문을 받으며 빈 객체만 허용한다.
func (h *SchedulePosterHandler) CollectSchedulePosters(c *gin.Context) {
	if h == nil || h.collector == nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "schedule poster collection unavailable", nil)

		return
	}

	var request *struct{}

	if !decodeSchedulePosterChange(c, &request) {
		return
	}

	if request == nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"queued": h.collector.RequestCollect()})
}

func (h *SchedulePosterHandler) RejectSchedulePoster(c *gin.Context) {
	var request schedulePosterRevisionRequest

	h.change(c, &request, &request.ExpectedRevision, func(ctx context.Context, id, revision int64) (int64, error) {
		return h.store.Reject(ctx, id, revision)
	})
}

func (h *SchedulePosterHandler) RetranslateSchedulePoster(c *gin.Context) {
	var request schedulePosterRevisionRequest

	h.change(c, &request, &request.ExpectedRevision, func(ctx context.Context, id, revision int64) (int64, error) {
		return h.store.RequestTranslation(ctx, id, revision)
	})
}

// change의 request와 expectedRevision은 같은 요청 구조체를 가리킨다. 본문을 디코드한 뒤에 revision을 읽는다.
func (h *SchedulePosterHandler) change(
	c *gin.Context,
	request any,
	expectedRevision *string,
	apply func(ctx context.Context, id, revision int64) (int64, error),
) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	if !decodeSchedulePosterChange(c, request) {
		return
	}

	revision, err := strconv.ParseInt(*expectedRevision, 10, 64)
	if err != nil || revision <= 0 {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	next, err := apply(ctx, id, revision)
	if err != nil {
		h.respondStoreError(c, "change schedule poster", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"id": strconv.FormatInt(id, 10), "revision": strconv.FormatInt(next, 10)})
}

func decodeSchedulePosterChange(c *gin.Context, request any) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		sharedserver.RespondError(c, http.StatusUnsupportedMediaType, "application/json required", nil)

		return false
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, schedulePosterMaxBody)
	if err := jsonv2.UnmarshalRead(c.Request.Body, request, jsonv2.RejectUnknownMembers(true)); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)

		return false
	}

	return true
}
