package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const broadcastTimeTestMember = "사쿠라 미코"

func newBroadcastTimeTestDeps(repo *stubBroadcastHistoryRepository, sent *[]string) *handlercore.Dependencies {
	memberProvider := newTrackedMemberProvider(&domain.Member{ChannelID: testChannelMiko, Name: broadcastTimeTestMember})

	return &handlercore.Dependencies{
		BroadcastHistory: repo,
		Formatter:        formatter.NewResponseFormatter("!", nil),
		Matcher:          matcher.NewMatcher(memberProvider, slog.New(slog.DiscardHandler)),
		SendMessage: func(_ context.Context, _, message string) error {
			*sent = append(*sent, message)
			return nil
		},
		SendError: func(_ context.Context, _, key string) error {
			*sent = append(*sent, key)
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}
}

func TestBroadcastTimeCommandQueriesMemberPeriod(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		params   botcommand.BroadcastTimeParams
		wantDays int
	}{
		{botcommand.BroadcastTimeParams{Member: broadcastTimeTestMember}, defaultBroadcastTimeDays},
		{botcommand.BroadcastTimeParams{Member: broadcastTimeTestMember, Days: 30}, 30},
	} {
		repo := &stubBroadcastHistoryRepository{entries: []handlercore.BroadcastHistoryEntry{{VideoID: "AqxEw3kXcgU", BroadcastType: "game"}}}

		var sent []string

		before := time.Now().AddDate(0, 0, -tc.wantDays).Add(-2 * time.Second)

		err := NewBroadcastTimeCommand(newBroadcastTimeTestDeps(repo, &sent)).Execute(t.Context(),
			&botcommand.CommandContext{Room: testHistoryRoom}, tc.params)
		require.NoError(t, err)

		after := time.Now().AddDate(0, 0, -tc.wantDays).Add(2 * time.Second)

		require.Equal(t, 1, repo.listCalls)
		require.Equal(t, testChannelMiko, repo.listQuery.ChannelID)
		require.Equal(t, broadcastTimeMaxEntries, repo.listQuery.Limit)
		require.Empty(t, repo.listQuery.Type)
		require.True(t, !repo.listQuery.Since.Before(before) && !repo.listQuery.Since.After(after), repo.listQuery.Since)
		require.Len(t, sent, 1)
		require.Contains(t, sent[0], fmt.Sprintf("📅 사쿠라 미코 방송시간 · 최근 %d일\n합계 ", tc.wantDays))
	}
}

func TestBroadcastTimeCommandMarksPartialAtCap(t *testing.T) {
	t.Parallel()

	entries := make([]handlercore.BroadcastHistoryEntry, broadcastTimeMaxEntries)
	repo := &stubBroadcastHistoryRepository{entries: entries}

	var sent []string

	err := NewBroadcastTimeCommand(newBroadcastTimeTestDeps(repo, &sent)).Execute(t.Context(),
		&botcommand.CommandContext{Room: testHistoryRoom}, botcommand.BroadcastTimeParams{Member: broadcastTimeTestMember})
	require.NoError(t, err)
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], "조회 한도에 도달해 최근 방송 일부만 합산했습니다.")
}

func TestBroadcastTimeCommandWithoutMemberSendsUsage(t *testing.T) {
	t.Parallel()

	repo := &stubBroadcastHistoryRepository{}

	var sent []string

	err := NewBroadcastTimeCommand(newBroadcastTimeTestDeps(repo, &sent)).Execute(t.Context(),
		&botcommand.CommandContext{Room: testHistoryRoom}, botcommand.BroadcastTimeParams{})
	require.NoError(t, err)
	require.Zero(t, repo.listCalls)
	require.Equal(t, []string{formatter.NewResponseFormatter("!", nil).BroadcastTimeUsage()}, sent)
}

func TestBroadcastTimeCommandListErrorSendsOneUserMessage(t *testing.T) {
	t.Parallel()

	repo := &stubBroadcastHistoryRepository{listErr: errors.New("db down")}

	var sent []string

	err := NewBroadcastTimeCommand(newBroadcastTimeTestDeps(repo, &sent)).Execute(t.Context(),
		&botcommand.CommandContext{Room: testHistoryRoom}, botcommand.BroadcastTimeParams{Member: broadcastTimeTestMember})
	require.NoError(t, err)
	require.Equal(t, []string{messaging.ErrBroadcastTimeQueryFailed}, sent)
}
