package api

import (
	"context"
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
	schedulePosterZone              = time.FixedZone("JST", 9*60*60)
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
	now       func() time.Time
}

type schedulePosterRowsResponse struct {
	ID              string                       `json:"id"`
	Today           string                       `json:"today"`
	RowsExtractedAt *string                      `json:"rowsExtractedAt"`
	RowsDayShift    int                          `json:"rowsDayShift"`
	PublishStart    *string                      `json:"publishStart"`
	PublishEnd      *string                      `json:"publishEnd"`
	PublishedToday  bool                         `json:"publishedToday"`
	TodayPosterID   *string                      `json:"todayPosterId"`
	UpdatedAt       *string                      `json:"updatedAt"`
	Slots           []schedulePosterSlotResponse `json:"slots"`
}

type schedulePosterSlotResponse struct {
	Number      int                          `json:"number"`
	Ordinal     *int                         `json:"ordinal"`
	AddedPostID *string                      `json:"addedPostId"`
	Day         string                       `json:"day"`
	Start       string                       `json:"start"`
	End         string                       `json:"end"`
	Status      string                       `json:"status"`
	SourceZone  string                       `json:"sourceZone"`
	SourceText  string                       `json:"sourceText"`
	Badges      []string                     `json:"badges"`
	TitleKo     string                       `json:"titleKo"`
	WithKo      []string                     `json:"withKo"`
	PlaceKo     string                       `json:"placeKo"`
	TagsKo      []string                     `json:"tagsKo"`
	NoteKo      string                       `json:"noteKo"`
	DoubtKo     string                       `json:"doubtKo"`
	Warnings    []string                     `json:"warnings"`
	TimeChanged bool                         `json:"timeChanged"`
	OrigDay     *string                      `json:"origDay"`
	OrigStart   *string                      `json:"origStart"`
	MovedTo     *string                      `json:"movedTo"`
	Steps       []schedulePosterStepResponse `json:"steps"`
}

type schedulePosterStepResponse struct {
	PostID string `json:"postId"`
	Item   int    `json:"item"`
	Kind   string `json:"kind"`
}

type schedulePosterResponse struct {
	ID              string  `json:"id"`
	ChannelID       string  `json:"channelId"`
	PostURL         string  `json:"postUrl"`
	ImageURL        string  `json:"imageUrl"`
	PostedAt        string  `json:"postedAt"`
	TextExcerpt     string  `json:"textExcerpt"`
	ReviewStatus    string  `json:"reviewStatus"`
	WeekStart       *string `json:"weekStart"`
	WeekEnd         *string `json:"weekEnd"`
	ReviewedAt      *string `json:"reviewedAt"`
	ReviewedBy      string  `json:"reviewedBy"`
	ReviewReason    string  `json:"reviewRequestReason"`
	Revision        string  `json:"revision"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	RowsStatus      string  `json:"rowsStatus"`
	RowsError       string  `json:"rowsError"`
	RowsExtractedAt *string `json:"rowsExtractedAt"`
	RowsCount       int     `json:"rowsCount"`
}

type schedulePosterPostsResponse struct {
	Posts []schedulePosterPostResponse `json:"posts"`
}

type schedulePosterPostResponse struct {
	ID          string                         `json:"id"`
	PostURL     string                         `json:"postUrl"`
	PostedAt    string                         `json:"postedAt"`
	TextExcerpt string                         `json:"textExcerpt"`
	Status      string                         `json:"status"`
	Reason      string                         `json:"reason"`
	Current     bool                           `json:"current"`
	Revocable   bool                           `json:"revocable"`
	Restorable  bool                           `json:"restorable"`
	Changes     []schedulePosterChangeResponse `json:"changes"`
}

type schedulePosterChangeResponse struct {
	Item    int     `json:"item"`
	Kind    string  `json:"kind"`
	Reason  string  `json:"reason"`
	Slot    *int    `json:"slot"`
	Day     *string `json:"day"`
	Start   *string `json:"start"`
	Status  string  `json:"status"`
	Text    string  `json:"text"`
	TitleKo string  `json:"titleKo"`
}

type schedulePosterRevisionRequest struct {
	ExpectedRevision string `json:"expectedRevision"`
}

type schedulePosterPostRequest struct {
	PostID string `json:"postId"`
}

type schedulePosterWeekRequest struct {
	ExpectedRevision string `json:"expectedRevision"`
	WeekStart        string `json:"weekStart"`
	WeekEnd          string `json:"weekEnd"`
}

func newSchedulePosterResponse(poster *scheduleposter.Poster) schedulePosterResponse {
	return schedulePosterResponse{
		ID:              strconv.FormatInt(poster.ID, 10),
		ChannelID:       poster.ChannelID,
		PostURL:         poster.PostURL,
		ImageURL:        poster.ImageURL,
		PostedAt:        formatTimestamp(poster.PostedAt),
		TextExcerpt:     poster.TextExcerpt,
		ReviewStatus:    string(poster.ReviewStatus),
		WeekStart:       formatOptionalDate(poster.WeekStart),
		WeekEnd:         formatOptionalDate(poster.WeekEnd),
		ReviewedAt:      formatOptionalTimestamp(poster.ReviewedAt),
		ReviewedBy:      string(poster.ReviewedBy),
		ReviewReason:    string(poster.ReviewRequestReason),
		Revision:        strconv.FormatInt(poster.Revision, 10),
		CreatedAt:       formatTimestamp(poster.CreatedAt),
		UpdatedAt:       formatTimestamp(poster.UpdatedAt),
		RowsStatus:      string(poster.RowsStatus),
		RowsError:       poster.RowsError,
		RowsExtractedAt: formatOptionalTimestamp(poster.RowsExtractedAt),
		RowsCount:       poster.RowsCount,
	}
}

func newSchedulePosterPostResponse(post *scheduleposter.PosterPost) schedulePosterPostResponse {
	changes := make([]schedulePosterChangeResponse, 0, len(post.Changes))

	for i := range post.Changes {
		change := &post.Changes[i]
		item := schedulePosterChangeResponse{
			Item:    change.Item,
			Kind:    string(change.Kind),
			Reason:  change.Reason,
			Start:   change.Start,
			Status:  string(change.Status),
			Text:    change.Text,
			TitleKo: change.TitleKo,
		}

		if change.Slot > 0 {
			item.Slot = new(change.Slot)
		}

		if !change.Day.IsZero() {
			item.Day = new(change.Day.Format(time.DateOnly))
		}

		changes = append(changes, item)
	}

	return schedulePosterPostResponse{
		ID:          strconv.FormatInt(post.ID, 10),
		PostURL:     scheduleposter.PostURL(post.ID),
		PostedAt:    formatTimestamp(post.PostedAt),
		TextExcerpt: post.TextExcerpt,
		Status:      string(post.Status),
		Reason:      post.Reason,
		Current:     post.Current,
		Revocable:   post.Revocable,
		Restorable:  post.Restorable,
		Changes:     changes,
	}
}

func newSchedulePosterRowsResponse(schedule *scheduleposter.PosterSchedule, today time.Time) schedulePosterRowsResponse {
	slots := schedule.Slots()
	items := make([]schedulePosterSlotResponse, 0, len(slots))

	for i := range slots {
		items = append(items, newSchedulePosterSlotResponse(&slots[i]))
	}

	response := schedulePosterRowsResponse{
		ID:             strconv.FormatInt(schedule.PosterID, 10),
		Today:          today.Format(time.DateOnly),
		RowsDayShift:   schedule.RowsDayShift,
		PublishedToday: schedule.TodayPosterID == schedule.PosterID,
		Slots:          items,
	}

	if schedule.Confirmed {
		response.PublishStart = new(schedule.WeekStart.AddDate(0, 0, schedule.RowsDayShift).Format(time.DateOnly))
		response.PublishEnd = new(schedule.WeekEnd.AddDate(0, 0, schedule.RowsDayShift).Format(time.DateOnly))
	}

	if schedule.TodayPosterID != 0 {
		response.TodayPosterID = new(strconv.FormatInt(schedule.TodayPosterID, 10))
	}

	if !schedule.ExtractedAt.IsZero() {
		response.RowsExtractedAt = new(formatTimestamp(schedule.ExtractedAt))
		response.UpdatedAt = new(formatTimestamp(schedule.UpdatedAt()))
	}

	return response
}

func newSchedulePosterSlotResponse(slot *scheduleposter.Slot) schedulePosterSlotResponse {
	steps := make([]schedulePosterStepResponse, 0, len(slot.Steps))
	for i := range slot.Steps {
		step := &slot.Steps[i]

		steps = append(steps, schedulePosterStepResponse{PostID: strconv.FormatInt(step.PostID, 10), Item: step.Item, Kind: string(step.Kind)})
	}

	item := schedulePosterSlotResponse{
		Number:      slot.Number,
		Day:         slot.Day.Format(time.DateOnly),
		Start:       slot.Start,
		End:         slot.End,
		Status:      string(slot.Status),
		SourceZone:  slot.SourceZone,
		SourceText:  slot.SourceText,
		Badges:      slot.Badges,
		TitleKo:     slot.TitleKo,
		WithKo:      slot.WithKo,
		PlaceKo:     slot.PlaceKo,
		TagsKo:      slot.TagsKo,
		NoteKo:      slot.NoteKo,
		DoubtKo:     slot.DoubtKo,
		Warnings:    slot.Warnings,
		TimeChanged: slot.TimeChanged,
		Steps:       steps,
	}

	if slot.AddedPostID != 0 {
		item.AddedPostID = new(strconv.FormatInt(slot.AddedPostID, 10))
	} else {
		item.Ordinal = new(slot.Ordinal)
		item.OrigDay = new(slot.OrigDay.Format(time.DateOnly))
		item.OrigStart = new(slot.OrigStart)
	}

	if !slot.MovedTo.IsZero() {
		item.MovedTo = new(slot.MovedTo.Format(time.DateOnly))
	}

	return item
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
	id, ok := parseSchedulePosterID(c.Param("id"))
	if !ok {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster id", nil)

		return 0, false
	}

	return id, true
}

func parseSchedulePosterID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
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

// GetSchedulePosterImage는 포스터에 저장한 그림을 저장한 형식 그대로 보낸다. 그림을 저장하지 않은 포스터도 404다.
func (h *SchedulePosterHandler) GetSchedulePosterImage(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	image, stored, err := h.store.Image(ctx, id)
	if err != nil {
		h.respondStoreError(c, "get schedule poster image", err)

		return
	}

	if !stored {
		sharedserver.RespondError(c, http.StatusNotFound, "schedule poster image not stored", nil)

		return
	}

	c.Data(http.StatusOK, image.MIME, image.Data)
}

// GetSchedulePosterChanges는 포스터의 수집 조건에 드는 글과, 지금 행 묶음에 대응한 글의 변경을 돌려준다.
func (h *SchedulePosterHandler) GetSchedulePosterChanges(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	posts, err := h.store.PosterPosts(ctx, id)
	if err != nil {
		h.respondStoreError(c, "list schedule poster posts", err)

		return
	}

	items := make([]schedulePosterPostResponse, 0, len(posts))
	for i := range posts {
		items = append(items, newSchedulePosterPostResponse(&posts[i]))
	}

	ginjson.Respond(c, http.StatusOK, schedulePosterPostsResponse{Posts: items})
}

// GetSchedulePosterRows는 포스터의 행 묶음에 반영 변경을 적용한 칸과, 오늘(JST) !스케쥴이 이 포스터를 보여 주는지를 돌려준다.
func (h *SchedulePosterHandler) GetSchedulePosterRows(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	now := h.now().In(schedulePosterZone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	schedule, err := h.store.PosterRows(ctx, id, today)
	if err != nil {
		h.respondStoreError(c, "load schedule poster rows", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, newSchedulePosterRowsResponse(&schedule, today))
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

// UpdateSchedulePosterWeek은 확정본의 주 범위를 고치고 일정표 행을 다시 뽑는다.
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

// RequestSchedulePosterRows는 일정표 행만 다시 뽑게 예약하며 revision을 올리지 않는다.
func (h *SchedulePosterHandler) RequestSchedulePosterRows(c *gin.Context) {
	var request schedulePosterRevisionRequest

	h.change(c, &request, &request.ExpectedRevision, func(ctx context.Context, id, revision int64) (int64, error) {
		return h.store.RequestRows(ctx, id, revision)
	})
}

// RevokeSchedulePosterChange는 글 하나의 변경을 철회한다. Console gateway가 경로 변수로 {id}만 받으므로 글 id는 본문에 둔다.
func (h *SchedulePosterHandler) RevokeSchedulePosterChange(c *gin.Context) {
	h.changePost(c, "revoke schedule poster post", (*scheduleposter.Store).RevokePost)
}

// RestoreSchedulePosterChange는 철회한 글을 되살려 다음 주기에 다시 대응하게 한다.
func (h *SchedulePosterHandler) RestoreSchedulePosterChange(c *gin.Context) {
	h.changePost(c, "restore schedule poster post", (*scheduleposter.Store).RestorePost)
}

func (h *SchedulePosterHandler) changePost(
	c *gin.Context,
	action string,
	apply func(store *scheduleposter.Store, ctx context.Context, posterID, postID int64) error,
) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	var request schedulePosterPostRequest

	if !decodeSchedulePosterChange(c, &request) {
		return
	}

	postID, ok := parseSchedulePosterID(request.PostID)
	if !ok {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	if err := apply(h.store, ctx, id, postID); err != nil {
		h.respondStoreError(c, action, err)

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"id": strconv.FormatInt(id, 10), "postId": strconv.FormatInt(postID, 10)})
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
