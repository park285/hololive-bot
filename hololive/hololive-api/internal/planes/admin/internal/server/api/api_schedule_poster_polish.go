package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

const schedulePosterTextMaxBody = 64 << 10

type schedulePosterKoreanRequest struct {
	ExpectedUpdatedAt string   `json:"expectedUpdatedAt"`
	TitleKo           string   `json:"titleKo"`
	WithKo            []string `json:"withKo"`
	PlaceKo           string   `json:"placeKo"`
	TagsKo            []string `json:"tagsKo"`
	NoteKo            string   `json:"noteKo"`
}

type schedulePosterPolishRowRequest struct {
	schedulePosterKoreanRequest `json:",inline"`

	Ordinal int `json:"ordinal"`
}

type schedulePosterPolishAddedRequest struct {
	schedulePosterKoreanRequest `json:",inline"`

	PostID string `json:"postId"`
	Item   int    `json:"item"`
}

type schedulePosterMemoResponse struct {
	Body      string  `json:"body"`
	Revision  string  `json:"revision"`
	UpdatedAt *string `json:"updatedAt"`
}

type schedulePosterMemoRequest struct {
	ExpectedRevision string `json:"expectedRevision"`
	Body             string `json:"body"`
}

func (r *schedulePosterKoreanRequest) korean() scheduleposter.Korean {
	trimAll := func(items []string) []string {
		trimmed := make([]string, len(items))
		for i, item := range items {
			trimmed[i] = strings.TrimSpace(item)
		}

		return trimmed
	}

	return scheduleposter.Korean{
		TitleKo: strings.TrimSpace(r.TitleKo),
		WithKo:  trimAll(r.WithKo),
		PlaceKo: strings.TrimSpace(r.PlaceKo),
		TagsKo:  trimAll(r.TagsKo),
		NoteKo:  strings.TrimSpace(r.NoteKo),
	}
}

// PolishSchedulePosterRow는 포스터 행에서 온 칸 하나의 한국어 다섯 칸을 관리자가 고친 값으로 바꾼다. 칸은 일정표 응답의 ordinal로,
// 읽은 일정은 그 응답의 updatedAt으로 가리킨다. Console gateway가 경로 변수로 {id}만 받으므로 둘 다 본문에 둔다.
func (h *SchedulePosterHandler) PolishSchedulePosterRow(c *gin.Context) {
	var request schedulePosterPolishRowRequest

	h.polish(c, &request, &request.schedulePosterKoreanRequest, func() (scheduleposter.SlotKey, bool) {
		return scheduleposter.SlotKey{Ordinal: request.Ordinal}, request.Ordinal >= 1 && request.Ordinal <= scheduleposter.MaxRows
	})
}

// PolishSchedulePosterAddedSlot은 글이 더한 칸 하나를 PolishSchedulePosterRow처럼 바꾼다. 칸은 일정표 응답의 addedPostId·addedItem으로 가리킨다.
func (h *SchedulePosterHandler) PolishSchedulePosterAddedSlot(c *gin.Context) {
	var request schedulePosterPolishAddedRequest

	h.polish(c, &request, &request.schedulePosterKoreanRequest, func() (scheduleposter.SlotKey, bool) {
		postID, ok := parseSchedulePosterID(request.PostID)

		return scheduleposter.SlotKey{AddedPostID: postID, AddedItem: request.Item},
			ok && request.Item >= 1 && request.Item <= scheduleposter.MaxChangeItems
	})
}

// polish의 request와 fields는 같은 요청 구조체를 가리키고, slot은 본문을 디코드한 뒤에 부른다.
func (h *SchedulePosterHandler) polish(
	c *gin.Context, request any, fields *schedulePosterKoreanRequest, slot func() (scheduleposter.SlotKey, bool),
) {
	if !h.ready(c) {
		return
	}

	id, ok := h.posterID(c)
	if !ok {
		return
	}

	if !decodeSchedulePosterBody(c, request, schedulePosterTextMaxBody) {
		return
	}

	seen, err := time.Parse(time.RFC3339, fields.ExpectedUpdatedAt)
	key, ok := slot()

	if err != nil || !ok {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	if err := h.store.PolishSlot(ctx, id, seen, key, fields.korean()); err != nil {
		h.respondStoreError(c, "polish schedule poster slot", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"id": strconv.FormatInt(id, 10)})
}

// GetSchedulePosterMemo는 번역 지시문에 참고 절로 붙는 관리자 메모를 돌려준다.
func (h *SchedulePosterHandler) GetSchedulePosterMemo(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	memo, err := h.store.TranslationMemo(ctx)
	if err != nil {
		h.respondStoreError(c, "load schedule poster translation memo", err)

		return
	}

	response := schedulePosterMemoResponse{Body: memo.Body, Revision: strconv.FormatInt(memo.Revision, 10)}
	if !memo.UpdatedAt.IsZero() {
		response.UpdatedAt = new(formatTimestamp(memo.UpdatedAt))
	}

	ginjson.Respond(c, http.StatusOK, response)
}

// SaveSchedulePosterMemo는 메모 본문을 바꾼다. 줄바꿈은 \n으로 맞추고 앞뒤 공백은 뺀다. 저장한 메모는 다음 번역부터 쓰인다.
func (h *SchedulePosterHandler) SaveSchedulePosterMemo(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	var request schedulePosterMemoRequest

	if !decodeSchedulePosterBody(c, &request, schedulePosterTextMaxBody) {
		return
	}

	revision, err := strconv.ParseInt(request.ExpectedRevision, 10, 64)
	if err != nil || revision <= 0 {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule poster request body", nil)

		return
	}

	body := strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(request.Body))

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	next, err := h.store.SaveTranslationMemo(ctx, revision, body)
	if err != nil {
		h.respondStoreError(c, "save schedule poster translation memo", err)

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"revision": strconv.FormatInt(next, 10)})
}
