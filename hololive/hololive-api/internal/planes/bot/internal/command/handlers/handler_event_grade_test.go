package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

type gradeRegistration func(context.Context, contract.EventGradeRequest) (contract.EventGradeResponse, error)

func (f gradeRegistration) RegisterEventGrade(ctx context.Context, req contract.EventGradeRequest) (contract.EventGradeResponse, error) {
	return f(ctx, req)
}

func TestEventGradeAcknowledgesPinnedRelayWithoutEchoingUntrustedNames(t *testing.T) {
	var (
		reply string
		calls int
	)

	deps := &handlercore.Dependencies{SendMessage: func(_ context.Context, room, text string) error {
		require.Equal(t, "101", room)

		reply = text

		return nil
	}}
	end := time.Date(2099, time.January, 31, 1, 0, 0, 0, time.UTC)
	cmd := NewEventGradeCommand(deps, gradeRegistration(func(_ context.Context, q contract.EventGradeRequest) (contract.EventGradeResponse, error) {
		calls++

		require.Equal(t, contract.EventGradeRequest{Region: "as", RoomID: "101", UserID: "202", Score: 250}, q)

		return contract.EventGradeResponse{Status: "registered", Region: "as", EventType: "relay", EndsAt: end, RevealsAt: end.Add(time.Hour)}, nil
	}))
	ctx := &botcommand.CommandContext{Room: "101", UserID: "202", UserName: "never echo"}
	require.NoError(t, cmd.Execute(t.Context(), ctx, nil))
	require.Contains(t, reply, "사용법")
	require.Zero(t, calls)
	require.NoError(t, cmd.Execute(t.Context(), ctx, map[string]any{"region": contract.GradeRegion("as"), "score": int64(250)}))
	require.Equal(t, 1, calls)

	for _, part := range []string{"릴레이 마라톤", "250점", "종료까지", "01-31 10:00 KST", "01-31 11:00 KST"} {
		require.Contains(t, reply, part)
	}

	require.NotContains(t, reply, ctx.UserName)
}

func TestEventGradeReportsUnconfirmedRegistrationSeparately(t *testing.T) {
	var reply string

	deps := &handlercore.Dependencies{SendMessage: func(_ context.Context, _, text string) error {
		reply = text

		return nil
	}}
	cmd := NewEventGradeCommand(deps, gradeRegistration(func(context.Context, contract.EventGradeRequest) (contract.EventGradeResponse, error) {
		return contract.EventGradeResponse{}, context.DeadlineExceeded
	}))
	ctx := &botcommand.CommandContext{Room: "101", UserID: "202"}

	require.NoError(t, cmd.Execute(t.Context(), ctx, map[string]any{"region": contract.GradeRegion("jp"), "score": int64(250)}))
	require.Contains(t, reply, "등록 결과를 확인하지 못했습니다")
	require.Contains(t, reply, "다시 입력하면")
}
