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

func (h *SchedulePosterHandler) PolishSchedulePosterRow(c *gin.Context) {
	var request schedulePosterPolishRowRequest

	h.polish(c, &request, &request.schedulePosterKoreanRequest, func() (scheduleposter.SlotKey, bool) {
		return scheduleposter.SlotKey{Ordinal: request.Ordinal}, request.Ordinal >= 1 && request.Ordinal <= scheduleposter.MaxRows
	})
}

func (h *SchedulePosterHandler) PolishSchedulePosterAddedSlot(c *gin.Context) {
	var request schedulePosterPolishAddedRequest

	h.polish(c, &request, &request.schedulePosterKoreanRequest, func() (scheduleposter.SlotKey, bool) {
		postID, ok := parseSchedulePosterID(request.PostID)

		return scheduleposter.SlotKey{AddedPostID: postID, AddedItem: request.Item},
			ok && request.Item >= 1 && request.Item <= scheduleposter.MaxChangeItems
	})
}

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
