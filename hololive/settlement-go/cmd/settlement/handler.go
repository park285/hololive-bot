package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/park285/iris-client-go/iris"

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

type parsedCommand struct {
	Action           string
	ExplicitCycleKey string
}

type botHandler struct {
	svc        *settlement.Service
	iris       iris.Sender
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

	if !b.allowRooms[req.Room] {
		w.WriteHeader(http.StatusOK)
		return
	}

	cmd := b.parseCommand(req.Text)
	if cmd.Action == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	b.logger.Info("Command received",
		slog.String("action", cmd.Action),
		slog.String("room", req.Room),
		slog.String("user_id", req.UserID),
		slog.String("explicit_cycle_key", cmd.ExplicitCycleKey),
	)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	threadID := resolveThreadID(req)

	var opts []iris.SendOption
	if threadID != "" {
		opts = append(opts, iris.WithThreadID(threadID))
	}

	var msg string
	var err error

	switch cmd.Action {
	case "status":
		msg, err = b.handleStatus(ctx, req.Room)
	case "paid":
		msg, err = b.handlePaid(ctx, req.Room, req.UserID, cmd.ExplicitCycleKey, strings.TrimSpace(req.ChatLogID))
	}

	if err != nil {
		b.logger.Error("처리 실패", slog.String("action", cmd.Action), slog.String("error", err.Error()))
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

func (b *botHandler) parseCommand(text string) parsedCommand {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") {
		return parsedCommand{}
	}

	cmd := strings.TrimSpace(text[1:])

	switch {
	case cmd == "정산":
		return parsedCommand{Action: "status"}
	case cmd == "정산완료":
		return parsedCommand{Action: "paid"}
	case strings.HasPrefix(cmd, "정산완료 "):
		return parsedCommand{Action: "paid", ExplicitCycleKey: strings.TrimSpace(strings.TrimPrefix(cmd, "정산완료 "))}
	case cmd == "정산 완료":
		return parsedCommand{Action: "paid"}
	case strings.HasPrefix(cmd, "정산 완료 "):
		return parsedCommand{Action: "paid", ExplicitCycleKey: strings.TrimSpace(strings.TrimPrefix(cmd, "정산 완료 "))}
	default:
		return parsedCommand{}
	}
}

func (b *botHandler) handleStatus(ctx context.Context, room string) (string, error) {
	cycle, statuses, err := b.svc.GetStatus(ctx, room, time.Now().UTC())
	if err != nil {
		if errors.Is(err, settlement.ErrNoActiveMembers) {
			return "❌ 이 방에 활성 정산 멤버가 없습니다.", nil
		}
		return "", fmt.Errorf("정산 현황 조회 실패: %w", err)
	}

	return b.formatter.formatStatus(cycle, statuses), nil
}

func (b *botHandler) handlePaid(ctx context.Context, room, userID, explicitCycleKey, sourceEventID string) (string, error) {
	cycle, statuses, err := b.svc.MarkPaid(ctx, settlement.MarkPaidInput{
		RoomID:           room,
		KakaoUserID:      userID,
		ExplicitCycleKey: explicitCycleKey,
		SourceType:       "kakao",
		SourceEventID:    sourceEventID,
		PaidAt:           time.Now().UTC(),
	})
	if err != nil {
		switch {
		case errors.Is(err, settlement.ErrNotRegisteredMember):
			return "❌ 이 방에 등록된 정산 멤버가 아닙니다.", nil
		case errors.Is(err, settlement.ErrNoPendingCycle):
			return "✅ 처리할 미납 회차가 없습니다.", nil
		case errors.Is(err, settlement.ErrInvalidExplicitCycle):
			return "❌ 회차 형식이 올바르지 않습니다. 예: !정산완료 2026-03-18 또는 !정산완료 3/18", nil
		case errors.Is(err, settlement.ErrFutureCycleNotAllowed):
			return "❌ 미래 회차에는 정산완료를 기록할 수 없습니다.", nil
		case errors.Is(err, settlement.ErrCycleNotFoundForMember):
			return "❌ 해당 회차의 정산 대상이 아닙니다.", nil
		case errors.Is(err, settlement.ErrNoActiveMembers):
			return "❌ 이 방에 활성 정산 멤버가 없습니다.", nil
		default:
			var multiErr *settlement.MultiplePendingCyclesError
			if errors.As(err, &multiErr) {
				var sb strings.Builder
				sb.WriteString("❌ 미납 회차가 여러 개라 자동으로 처리할 수 없습니다.\n")
				sb.WriteString("아래처럼 회차를 지정해주세요:\n")
				for _, cycleKey := range multiErr.CycleKeys {
					fmt.Fprintf(&sb, "- !정산완료 %s\n", cycleKey)
				}
				return strings.TrimRight(sb.String(), "\n"), nil
			}
			return "", fmt.Errorf("정산 완료 처리 실패: %w", err)
		}
	}

	return b.formatter.formatStatus(cycle, statuses), nil
}

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
