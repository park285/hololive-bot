package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/iris"

	"github.com/kapu/settlement-go/pkg/settlement"
)

type webhookRequest struct {
	Text       string `json:"text"`
	Room       string `json:"room"`
	Sender     string `json:"sender"`
	UserID     string `json:"userId"`
	ThreadID   string `json:"threadId"`
	ChatLogID  string `json:"chatLogId,omitempty"`
	RoomType   string `json:"roomType,omitempty"`
	RoomLinkID string `json:"roomLinkId,omitempty"`
}

type botHandler struct {
	repo       *settlement.Repository
	iris       iris.Client
	formatter  *messageFormatter
	allowRooms map[string]bool
	logger     *slog.Logger
}

func (b *botHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req webhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 화이트리스트 ACL
	if !b.allowRooms[req.Room] {
		w.WriteHeader(http.StatusOK)
		return
	}

	action := b.parseAction(req.Text)
	if action == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	b.logger.Info("Command received",
		slog.String("action", action),
		slog.String("room", req.Room),
		slog.String("user_id", req.UserID),
	)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// 오픈톡 threadID fallback: chatLogID → threadID
	threadID := resolveThreadID(req)

	var opts []iris.SendOption
	if threadID != "" {
		opts = append(opts, iris.WithThreadID(threadID))
	}

	var msg string
	var err error

	switch action {
	case "status":
		msg, err = b.handleStatus(ctx, req.Room)
	case "paid":
		msg, err = b.handlePaid(ctx, req.Room, req.UserID)
	}

	if err != nil {
		b.logger.Error("처리 실패", slog.String("action", action), slog.String("error", err.Error()))
		w.WriteHeader(http.StatusOK)
		return
	}

	if msg != "" {
		if sendErr := b.iris.SendMessage(ctx, req.Room, msg, opts...); sendErr != nil {
			b.logger.Error("메시지 발송 실패", slog.String("error", sendErr.Error()))
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (b *botHandler) parseAction(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") {
		return ""
	}

	cmd := strings.TrimSpace(text[1:])

	// !정산완료 또는 !정산 완료
	if cmd == "정산완료" {
		return "paid"
	}
	if strings.HasPrefix(cmd, "정산") {
		rest := strings.TrimSpace(strings.TrimPrefix(cmd, "정산"))
		if rest == "완료" {
			return "paid"
		}
		if rest == "" {
			return "status"
		}
	}

	return ""
}

func (b *botHandler) handleStatus(ctx context.Context, room string) (string, error) {
	kst, _ := time.LoadLocation("Asia/Seoul")
	now := time.Now().In(kst)
	year, month := now.Year(), int(now.Month())

	cycle, err := b.repo.EnsureCycle(ctx, room, year, month)
	if err != nil {
		return "", fmt.Errorf("사이클 확보 실패: %w", err)
	}

	if err := b.repo.EnsurePaymentRows(ctx, room, cycle.ID); err != nil {
		return "", fmt.Errorf("납부 행 생성 실패: %w", err)
	}

	statuses, err := b.repo.GetPaymentStatuses(ctx, cycle.ID)
	if err != nil {
		return "", fmt.Errorf("납부 상태 조회 실패: %w", err)
	}

	return b.formatter.formatStatus(cycle, statuses), nil
}

func (b *botHandler) handlePaid(ctx context.Context, room, userID string) (string, error) {
	kst, _ := time.LoadLocation("Asia/Seoul")
	now := time.Now().In(kst)
	year, month := now.Year(), int(now.Month())

	cycle, err := b.repo.EnsureCycle(ctx, room, year, month)
	if err != nil {
		return "", fmt.Errorf("사이클 확보 실패: %w", err)
	}

	if err := b.repo.EnsurePaymentRows(ctx, room, cycle.ID); err != nil {
		return "", fmt.Errorf("납부 행 생성 실패: %w", err)
	}

	member, err := b.repo.FindMemberByUserID(ctx, room, userID)
	if err != nil {
		return "", fmt.Errorf("멤버 조회 실패: %w", err)
	}

	if member == nil {
		return "❌ 이 방에 등록된 정산 멤버가 아닙니다.", nil
	}

	if err := b.repo.MarkPaid(ctx, cycle.ID, member.ID); err != nil {
		return "", fmt.Errorf("납부 처리 실패: %w", err)
	}

	statuses, err := b.repo.GetPaymentStatuses(ctx, cycle.ID)
	if err != nil {
		return "", fmt.Errorf("납부 상태 조회 실패: %w", err)
	}

	return b.formatter.formatStatus(cycle, statuses), nil
}

// resolveThreadID: 오픈톡에서 threadID가 없으면 chatLogID를 사용합니다.
func resolveThreadID(req webhookRequest) string {
	if id := strings.TrimSpace(req.ThreadID); id != "" {
		return id
	}

	chatLogID := strings.TrimSpace(req.ChatLogID)
	if chatLogID == "" {
		return ""
	}

	roomType := strings.TrimSpace(req.RoomType)
	roomLinkID := strings.TrimSpace(req.RoomLinkID)
	if strings.EqualFold(roomType, "OD") || roomLinkID != "" {
		return chatLogID
	}

	return ""
}
