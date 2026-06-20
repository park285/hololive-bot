// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package milestonescheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/stretchr/testify/require"
)

// mockMemberDataProvider: 테스트용 MemberDataProvider 구현
type mockMemberDataProvider struct {
	members []*domain.Member
}

func (m *mockMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range m.members {
		if member.ChannelID == channelID {
			return member
		}
	}
	return nil
}

func (m *mockMemberDataProvider) FindMemberByName(name string) *domain.Member {
	for _, member := range m.members {
		if member.Name == name {
			return member
		}
	}
	return nil
}

func (m *mockMemberDataProvider) FindMemberByAlias(alias string) *domain.Member {
	return nil
}

func (m *mockMemberDataProvider) GetChannelIDs() []string {
	ids := make([]string, len(m.members))
	for i, member := range m.members {
		ids[i] = member.ChannelID
	}
	return ids
}

func (m *mockMemberDataProvider) GetAllMembers() []*domain.Member {
	return m.members
}

func (m *mockMemberDataProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	return m
}

func (m *mockMemberDataProvider) FindMembersByName(name string) []*domain.Member {
	return nil
}

func (m *mockMemberDataProvider) FindMembersByAlias(alias string) []*domain.Member {
	return nil
}

// testMembers: 테스트용 멤버 데이터
func testMembers() []*domain.Member {
	return []*domain.Member{
		{ChannelID: "UC1", Name: "TestMember1"},
		{ChannelID: "UC2", Name: "TestMember2"},
		{ChannelID: "UC3", Name: "TestMember3"},
	}
}

func requireSchedulerImpl(t *testing.T, s Scheduler) *schedulerImpl {
	t.Helper()

	impl, ok := s.(*schedulerImpl)
	if !ok || impl == nil {
		t.Fatalf("expected *schedulerImpl, got %T", s)
	}
	return impl
}

func TestNewScheduler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	if scheduler == nil {
		t.Fatal("expected scheduler to be created, got nil")
	}
	if scheduler.currentBatch != 0 {
		t.Errorf("expected currentBatch to be 0, got %d", scheduler.currentBatch)
	}
	if scheduler.stopCh == nil {
		t.Error("expected stopCh to be initialized")
	}
}

func TestScheduler_Stop_IsIdempotent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	scheduler.Stop()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stop() panicked on second call: %v", r)
		}
	}()

	scheduler.Stop()
}

func TestScheduler_CheckMilestones(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	testCases := []struct {
		name         string
		prevCount    uint64
		currentCount uint64
		wantCount    int
		wantValues   []uint64
	}{
		{
			name:         "100k milestone crossed",
			prevCount:    99000,
			currentCount: 101000,
			wantCount:    1,
			wantValues:   []uint64{100000},
		},
		{
			name:         "no milestone crossed",
			prevCount:    100000,
			currentCount: 110000,
			wantCount:    0,
			wantValues:   []uint64{},
		},
		{
			name:         "multiple milestones crossed",
			prevCount:    240000,
			currentCount: 510000,
			wantCount:    2,
			wantValues:   []uint64{250000, 500000},
		},
		{
			name:         "1M milestone crossed",
			prevCount:    999000,
			currentCount: 1010000,
			wantCount:    1,
			wantValues:   []uint64{1000000},
		},
		{
			name:         "exact milestone boundary",
			prevCount:    249999,
			currentCount: 250000,
			wantCount:    1,
			wantValues:   []uint64{250000},
		},
		{
			name:         "decrease in subscribers",
			prevCount:    110000,
			currentCount: 95000,
			wantCount:    0,
			wantValues:   []uint64{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			milestones := scheduler.checkMilestones(tc.prevCount, tc.currentCount)

			if len(milestones) != tc.wantCount {
				t.Errorf("expected %d milestones, got %d", tc.wantCount, len(milestones))
			}

			for i, want := range tc.wantValues {
				if i < len(milestones) && milestones[i] != want {
					t.Errorf("expected milestone[%d] = %d, got %d", i, want, milestones[i])
				}
			}
		})
	}
}

func TestScheduler_GetRotatingBatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 5명의 멤버로 테스트 (배치 크기보다 작은 경우)
	smallMembers := &mockMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "UC1", Name: "Member1"},
			{ChannelID: "UC2", Name: "Member2"},
			{ChannelID: "UC3", Name: "Member3"},
			{ChannelID: "UC4", Name: "Member4"},
			{ChannelID: "UC5", Name: "Member5"},
		},
	}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, smallMembers, nil, nil, nil, logger))

	testCases := []struct {
		name      string
		batchNum  int
		batchSize int
		wantLen   int
	}{
		{
			name:      "batch 0 with size 2",
			batchNum:  0,
			batchSize: 2,
			wantLen:   2,
		},
		{
			name:      "batch 1 with size 2",
			batchNum:  1,
			batchSize: 2,
			wantLen:   2,
		},
		{
			name:      "batch 2 wraps around",
			batchNum:  2,
			batchSize: 2,
			wantLen:   2,
		},
		{
			name:      "batch size larger than total",
			batchNum:  0,
			batchSize: 10,
			wantLen:   10,
		},
		{
			name:      "batch size of 0",
			batchNum:  0,
			batchSize: 0,
			wantLen:   0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			batch := scheduler.getRotatingBatch(tc.batchNum, tc.batchSize)

			if len(batch) != tc.wantLen {
				t.Errorf("expected batch length %d, got %d", tc.wantLen, len(batch))
			}
		})
	}
}

func TestScheduler_GetRotatingBatch_EmptyMembers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	emptyMembers := &mockMemberDataProvider{members: []*domain.Member{}}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, emptyMembers, nil, nil, nil, logger))

	batch := scheduler.getRotatingBatch(0, 10)
	if len(batch) != 0 {
		t.Errorf("expected empty batch for empty members, got %d", len(batch))
	}
}

func TestScheduler_BuildChannelMaps(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	channelIDs, channelToMember := scheduler.buildChannelMaps()

	if len(channelIDs) != 3 {
		t.Errorf("expected 3 channel IDs, got %d", len(channelIDs))
	}

	if len(channelToMember) != 3 {
		t.Errorf("expected 3 channel-to-member mappings, got %d", len(channelToMember))
	}

	// 매핑 검증
	if member := channelToMember["UC1"]; member == nil || member.Name != "TestMember1" {
		t.Error("expected UC1 to map to TestMember1")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	ctx := t.Context()

	// Start 호출 시 패닉이 발생하지 않아야 함
	scheduler.Start(ctx)

	// ticker가 초기화되어야 함
	if scheduler.ticker == nil {
		t.Error("expected ticker to be initialized after Start")
	}

	// Stop 호출 시 정상 종료
	scheduler.Stop()

	// 채널이 닫혀야 함 (다시 Stop 호출 시 panic 방지)
	// stopCh가 닫힌 상태인지 확인
	select {
	case <-scheduler.stopCh:
		// 채널이 닫힘 - 정상
	default:
		t.Error("expected stopCh to be closed after Stop")
	}
}

func TestScheduler_IsSignificantChange(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	testCases := []struct {
		name   string
		change *domain.StatsChange
		want   bool
	}{
		{
			name: "large subscriber increase is not significant",
			change: &domain.StatsChange{
				SubscriberChange: 15000,
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: 110000},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: 125000},
			},
			want: false,
		},
		{
			name: "small subscriber increase",
			change: &domain.StatsChange{
				SubscriberChange: 100,
			},
			want: false,
		},
		{
			name: "milestone crossed",
			change: &domain.StatsChange{
				SubscriberChange: 5000,
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: 99000},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: 101000},
			},
			want: true,
		},
		{
			name: "no significant change",
			change: &domain.StatsChange{
				SubscriberChange: 500,
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: 110000},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: 110500},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := scheduler.isSignificantChange(tc.change)
			if got != tc.want {
				t.Errorf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestScheduler_FormatChangeMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	testCases := []struct {
		name      string
		change    *domain.StatsChange
		wantEmpty bool
		contains  string
	}{
		{
			name: "milestone message",
			change: &domain.StatsChange{
				MemberName:       "TestMember1",
				SubscriberChange: 5000,
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: 99000},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: 101000},
			},
			wantEmpty: false,
			contains:  "🎉",
		},
		{
			name: "no message for large gain without milestone",
			change: &domain.StatsChange{
				MemberName:       "TestMember1",
				SubscriberChange: 15000,
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: 110000},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: 125000},
			},
			wantEmpty: true,
		},
		{
			name: "no message for small change",
			change: &domain.StatsChange{
				MemberName:       "TestMember1",
				SubscriberChange: 100,
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: 110000},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: 110100},
			},
			wantEmpty: true,
		},
		{
			name: "no message for nil stats",
			change: &domain.StatsChange{
				MemberName:       "TestMember1",
				SubscriberChange: 15000,
			},
			wantEmpty: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := scheduler.formatChangeMessage(tc.change)

			if tc.wantEmpty {
				if msg != "" {
					t.Errorf("expected empty message, got: %s", msg)
				}
			} else {
				if msg == "" {
					t.Error("expected non-empty message")
				}
				if tc.contains != "" && !containsStr(msg, tc.contains) {
					t.Errorf("expected message to contain %q, got: %s", tc.contains, msg)
				}
			}
		})
	}
}

func TestCalculateStatsChanges(t *testing.T) {
	testCases := []struct {
		name     string
		prev     *domain.TimestampedStats
		current  *ChannelStats
		wantSub  int64
		wantVid  int64
		wantView int64
	}{
		{
			name: "all increases",
			prev: &domain.TimestampedStats{
				SubscriberCount: 100000,
				VideoCount:      50,
				ViewCount:       1000000,
			},
			current: &ChannelStats{
				SubscriberCount: 110000,
				VideoCount:      55,
				ViewCount:       1100000,
			},
			wantSub:  10000,
			wantVid:  5,
			wantView: 100000,
		},
		{
			name: "subscriber decrease",
			prev: &domain.TimestampedStats{
				SubscriberCount: 100000,
				VideoCount:      50,
				ViewCount:       1000000,
			},
			current: &ChannelStats{
				SubscriberCount: 99000,
				VideoCount:      50,
				ViewCount:       1010000,
			},
			wantSub:  -1000,
			wantVid:  0,
			wantView: 10000,
		},
		{
			name: "no change",
			prev: &domain.TimestampedStats{
				SubscriberCount: 100000,
				VideoCount:      50,
				ViewCount:       1000000,
			},
			current: &ChannelStats{
				SubscriberCount: 100000,
				VideoCount:      50,
				ViewCount:       1000000,
			},
			wantSub:  0,
			wantVid:  0,
			wantView: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subChange, vidChange, viewChange := calculateStatsChanges(tc.prev, tc.current)

			if subChange != tc.wantSub {
				t.Errorf("subscriber change: expected %d, got %d", tc.wantSub, subChange)
			}
			if vidChange != tc.wantVid {
				t.Errorf("video change: expected %d, got %d", tc.wantVid, vidChange)
			}
			if viewChange != tc.wantView {
				t.Errorf("view change: expected %d, got %d", tc.wantView, viewChange)
			}
		})
	}
}

func TestCreateTimestampedStats(t *testing.T) {
	member := &domain.Member{
		ChannelID: "UC123",
		Name:      "TestMember",
	}

	stats := &ChannelStats{
		SubscriberCount: 500000,
		VideoCount:      100,
		ViewCount:       10000000,
	}

	timestamp := time.Now()

	result := createTimestampedStats("UC123", member, stats, timestamp)

	if result.ChannelID != "UC123" {
		t.Errorf("expected ChannelID UC123, got %s", result.ChannelID)
	}
	if result.MemberName != "TestMember" {
		t.Errorf("expected MemberName TestMember, got %s", result.MemberName)
	}
	if result.SubscriberCount != 500000 {
		t.Errorf("expected SubscriberCount 500000, got %d", result.SubscriberCount)
	}
	if result.VideoCount != 100 {
		t.Errorf("expected VideoCount 100, got %d", result.VideoCount)
	}
	if result.ViewCount != 10000000 {
		t.Errorf("expected ViewCount 10000000, got %d", result.ViewCount)
	}
	if !result.Timestamp.Equal(timestamp) {
		t.Errorf("expected Timestamp %v, got %v", timestamp, result.Timestamp)
	}
}

// containsStr: 문자열 포함 여부 확인 헬퍼
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsStrHelper(s, substr))
}

func containsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockStatsRepository: SendMilestoneAlerts 테스트용 mock repository
type mockStatsRepository struct {
	changes          []*domain.StatsChange
	notifiedChannels []string
	savedMilestones  []*domain.Milestone
}

func (m *mockStatsRepository) GetUnnotifiedChanges(ctx context.Context, limit int) ([]*domain.StatsChange, error) {
	if len(m.changes) > limit {
		return m.changes[:limit], nil
	}
	return m.changes, nil
}

func (m *mockStatsRepository) MarkChangeNotified(ctx context.Context, channelID string, detectedAt time.Time) error {
	m.notifiedChannels = append(m.notifiedChannels, channelID)
	return nil
}

func (m *mockStatsRepository) GetLatestStats(ctx context.Context, channelID string) (*domain.TimestampedStats, error) {
	return nil, nil
}

func (m *mockStatsRepository) SaveStats(ctx context.Context, stats *domain.TimestampedStats) error {
	return nil
}

func (m *mockStatsRepository) SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	return nil
}

func (m *mockStatsRepository) RecordChange(ctx context.Context, change *domain.StatsChange) error {
	return nil
}

func (m *mockStatsRepository) SaveMilestone(ctx context.Context, milestone *domain.Milestone) error {
	m.savedMilestones = append(m.savedMilestones, milestone)
	return nil
}

func (m *mockStatsRepository) GetTopGainers(ctx context.Context, since time.Time, limit int) ([]domain.RankEntry, error) {
	return nil, nil
}

func TestSendMilestoneAlerts_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	// 마일스톤 달성 변경사항 (99000 → 101000, 100k 돌파)
	milestoneChange := &domain.StatsChange{
		ChannelID:        "UC1",
		MemberName:       "TestMember1",
		SubscriberChange: 2000,
		PreviousStats:    &domain.TimestampedStats{SubscriberCount: 99000},
		CurrentStats:     &domain.TimestampedStats{SubscriberCount: 101000},
		DetectedAt:       time.Now(),
	}

	// 큰 구독자 증가 (마일스톤 없음, 15000명 증가)
	largeGainChange := &domain.StatsChange{
		ChannelID:        "UC2",
		MemberName:       "TestMember2",
		SubscriberChange: 15000,
		PreviousStats:    &domain.TimestampedStats{SubscriberCount: 110000},
		CurrentStats:     &domain.TimestampedStats{SubscriberCount: 125000},
		DetectedAt:       time.Now(),
	}

	// 작은 변화 (알림 불필요)
	smallChange := &domain.StatsChange{
		ChannelID:        "UC3",
		MemberName:       "TestMember3",
		SubscriberChange: 100,
		PreviousStats:    &domain.TimestampedStats{SubscriberCount: 50000},
		CurrentStats:     &domain.TimestampedStats{SubscriberCount: 50100},
		DetectedAt:       time.Now(),
	}

	// 향후 SendMilestoneAlerts 통합 테스트 시 사용 예정
	_ = &mockStatsRepository{
		changes: []*domain.StatsChange{milestoneChange, largeGainChange, smallChange},
	}

	// 실제 Scheduler 대신 mock repo를 사용하는 테스트용 구조체 필요
	// 여기서는 로직만 테스트
	scheduler := &schedulerImpl{
		membersData: mockMembers,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}

	// 메시지 수집용
	var sentMessages []struct {
		room    string
		message string
	}

	sendMessageFunc := func(room, message string) error {
		if message == "" {
			return errors.New("empty message")
		}
		sentMessages = append(sentMessages, struct {
			room    string
			message string
		}{room, message})
		return nil
	}

	rooms := []string{"testRoom1", "testRoom2"}

	// 직접 로직 테스트 (statsRepo가 nil이므로 SendMilestoneAlerts 호출 불가)
	// 대신 개별 로직 검증

	assertMilestoneIntegrationMessages(t, scheduler, milestoneChange, largeGainChange, smallChange)

	// 수동 메시지 발송 시뮬레이션
	for _, change := range []*domain.StatsChange{milestoneChange, largeGainChange} {
		message := scheduler.formatChangeMessage(change)
		if message != "" {
			for _, room := range rooms {
				require.NoError(t, sendMessageFunc(room, message))
			}
		}
	}

	assertMilestoneIntegrationDispatch(t, sentMessages)
}

func assertMilestoneIntegrationMessages(
	t *testing.T,
	scheduler *schedulerImpl,
	milestoneChange, largeGainChange, smallChange *domain.StatsChange,
) {
	t.Helper()

	if !scheduler.isSignificantChange(milestoneChange) {
		t.Error("milestone change should be significant")
	}

	msg := scheduler.formatChangeMessage(milestoneChange)
	if msg == "" {
		t.Error("expected milestone message, got empty")
	}
	if !containsStr(msg, "🎉") {
		t.Errorf("milestone message should contain celebration emoji, got: %s", msg)
	}
	if !containsStr(msg, "TestMember1") {
		t.Errorf("milestone message should contain member name, got: %s", msg)
	}

	if msg := scheduler.formatChangeMessage(largeGainChange); msg != "" {
		t.Errorf("expected no large gain message, got: %s", msg)
	}
	if msg := scheduler.formatChangeMessage(smallChange); msg != "" {
		t.Errorf("small change should not generate message, got: %s", msg)
	}
	if scheduler.isSignificantChange(smallChange) {
		t.Error("small change should not be significant")
	}
}

func assertMilestoneIntegrationDispatch(t *testing.T, sentMessages []struct {
	room    string
	message string
}) {
	t.Helper()

	if len(sentMessages) != 2 {
		t.Errorf("expected 2 messages sent, got %d", len(sentMessages))
	}

	roomCounts := map[string]int{}
	for _, m := range sentMessages {
		roomCounts[m.room]++
	}
	if roomCounts["testRoom1"] != 1 || roomCounts["testRoom2"] != 1 {
		t.Errorf("expected 1 message per room, got room1=%d, room2=%d", roomCounts["testRoom1"], roomCounts["testRoom2"])
	}
}

func TestMilestoneDetectionFlow(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockMembers := &mockMemberDataProvider{members: testMembers()}

	scheduler := requireSchedulerImpl(t, NewScheduler(nil, nil, nil, nil, mockMembers, nil, nil, nil, logger))

	testCases := []struct {
		name            string
		prevSubs        uint64
		currentSubs     uint64
		expectMilestone bool
		expectEmoji     string
	}{
		{
			name:            "100k milestone",
			prevSubs:        99000,
			currentSubs:     101000,
			expectMilestone: true,
			expectEmoji:     "🎉",
		},
		{
			name:            "1M milestone",
			prevSubs:        999000,
			currentSubs:     1010000,
			expectMilestone: true,
			expectEmoji:     "🎉",
		},
		{
			name:            "2M milestone",
			prevSubs:        1990000,
			currentSubs:     2010000,
			expectMilestone: true,
			expectEmoji:     "🎉",
		},
		{
			name:            "no milestone but large gain",
			prevSubs:        110000,
			currentSubs:     125000,
			expectMilestone: false,
			expectEmoji:     "",
		},
		{
			name:            "no notification needed",
			prevSubs:        110000,
			currentSubs:     111000,
			expectMilestone: false,
			expectEmoji:     "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 마일스톤 검출
			milestones := scheduler.checkMilestones(tc.prevSubs, tc.currentSubs)

			if tc.expectMilestone && len(milestones) == 0 {
				t.Error("expected milestone to be detected")
			}
			if !tc.expectMilestone && len(milestones) > 0 {
				t.Errorf("unexpected milestone detected: %v", milestones)
			}

			// 메시지 생성
			change := &domain.StatsChange{
				MemberName:       "TestMember",
				SubscriberChange: uint64DeltaToInt64(tc.currentSubs, tc.prevSubs),
				PreviousStats:    &domain.TimestampedStats{SubscriberCount: tc.prevSubs},
				CurrentStats:     &domain.TimestampedStats{SubscriberCount: tc.currentSubs},
			}

			msg := scheduler.formatChangeMessage(change)

			if tc.expectEmoji == "" {
				if msg != "" {
					t.Errorf("expected no message, got: %s", msg)
				}
			} else {
				if !containsStr(msg, tc.expectEmoji) {
					t.Errorf("expected message with %s, got: %s", tc.expectEmoji, msg)
				}
			}
		})
	}
}

type mockTrackAllSubscribersService struct {
	stats map[string]*ChannelStats
	err   error
}

func (m *mockTrackAllSubscribersService) SetScraperProxyEnabled(enabled bool) bool { return enabled }
func (m *mockTrackAllSubscribersService) ScraperProxyEnabled() bool                { return false }
func (m *mockTrackAllSubscribersService) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	return m.stats, m.err
}
func (m *mockTrackAllSubscribersService) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error) {
	return nil, nil
}

type mockTrackAllSubscribersRepository struct {
	latestByChannel       map[string]*domain.TimestampedStats
	latestBatchErr        error
	latestBatchCalls      int
	latestBatchKeys       []string
	achievedByChannel     map[string][]uint64
	achievedErr           error
	achievedCalls         int
	hasAchievedCalls      int
	hasAchievedResult     bool
	saveBatchErr          error
	saveBatchCalls        int
	saveBatchRows         int
	saveSingleCalls       int
	saveMilestoneCalls    int
	recordChangeCalls     int
	unnotifiedMilestones  []ytstats.MilestoneNotification
	markedMilestones      []ytstats.MilestoneNotification
	unnotifiedApproaching []ytstats.ApproachingNotification
	markedApproaching     []ytstats.ApproachingNotification
}

func (m *mockTrackAllSubscribersRepository) GetLatestStats(ctx context.Context, channelID string) (*domain.TimestampedStats, error) {
	return m.latestByChannel[channelID], nil
}

func (m *mockTrackAllSubscribersRepository) GetLatestStatsForChannels(ctx context.Context, channelIDs []string) (map[string]*domain.TimestampedStats, error) {
	m.latestBatchCalls++
	m.latestBatchKeys = append([]string(nil), channelIDs...)
	if m.latestBatchErr != nil {
		return nil, m.latestBatchErr
	}
	return m.latestByChannel, nil
}

func (m *mockTrackAllSubscribersRepository) SaveStatsBatch(ctx context.Context, stats []*domain.TimestampedStats) error {
	m.saveBatchCalls++
	m.saveBatchRows += len(stats)
	return m.saveBatchErr
}

func (m *mockTrackAllSubscribersRepository) SaveStats(ctx context.Context, stats *domain.TimestampedStats) error {
	m.saveSingleCalls++
	return nil
}

func (m *mockTrackAllSubscribersRepository) RecordChange(ctx context.Context, change *domain.StatsChange) error {
	m.recordChangeCalls++
	return nil
}

func (m *mockTrackAllSubscribersRepository) RecordChangeBatch(ctx context.Context, changes []*domain.StatsChange) error {
	m.recordChangeCalls += len(changes)
	return nil
}

func (m *mockTrackAllSubscribersRepository) GetAchievedMilestones(ctx context.Context, channelIDs []string, milestoneType domain.MilestoneType) (map[string][]uint64, error) {
	m.achievedCalls++
	if m.achievedErr != nil {
		return nil, m.achievedErr
	}
	if m.achievedByChannel == nil {
		return map[string][]uint64{}, nil
	}
	return m.achievedByChannel, nil
}

func (m *mockTrackAllSubscribersRepository) HasAchievedMilestone(ctx context.Context, channelID string, milestoneType domain.MilestoneType, value uint64) (bool, error) {
	m.hasAchievedCalls++
	return m.hasAchievedResult, nil
}

func (m *mockTrackAllSubscribersRepository) SaveMilestone(ctx context.Context, milestone *domain.Milestone) error {
	m.saveMilestoneCalls++
	return nil
}

func (m *mockTrackAllSubscribersRepository) GetNearMilestoneMembers(ctx context.Context, thresholdPct float64, milestones []uint64, limit int) ([]ytstats.NearMilestoneEntry, error) {
	return nil, nil
}

func (m *mockTrackAllSubscribersRepository) HasApproachingNotified(ctx context.Context, channelID string, milestoneValue uint64) (bool, error) {
	return false, nil
}

func (m *mockTrackAllSubscribersRepository) SaveApproachingNotification(ctx context.Context, channelID string, milestoneValue, currentSubs uint64, notifiedAt time.Time) error {
	return nil
}

func (m *mockTrackAllSubscribersRepository) GetUnnotifiedMilestones(ctx context.Context, limit int) ([]ytstats.MilestoneNotification, error) {
	return append([]ytstats.MilestoneNotification(nil), m.unnotifiedMilestones...), nil
}

func (m *mockTrackAllSubscribersRepository) MarkMilestoneNotified(ctx context.Context, channelID, milestoneType string, value uint64) error {
	return nil
}

func (m *mockTrackAllSubscribersRepository) MarkMilestonesNotifiedBatch(ctx context.Context, milestones []ytstats.MilestoneNotification) error {
	m.markedMilestones = append([]ytstats.MilestoneNotification(nil), milestones...)
	return nil
}

func (m *mockTrackAllSubscribersRepository) GetUnnotifiedApproaching(ctx context.Context, limit int) ([]ytstats.ApproachingNotification, error) {
	return append([]ytstats.ApproachingNotification(nil), m.unnotifiedApproaching...), nil
}

func (m *mockTrackAllSubscribersRepository) MarkApproachingChatNotified(ctx context.Context, channelID string, milestoneValue uint64) error {
	return nil
}

func (m *mockTrackAllSubscribersRepository) MarkApproachingChatNotifiedBatch(ctx context.Context, notifications []ytstats.ApproachingNotification) error {
	m.markedApproaching = append([]ytstats.ApproachingNotification(nil), notifications...)
	return nil
}

type mockMilestoneFormatter struct{}

func (mockMilestoneFormatter) FormatMilestoneAchieved(ctx context.Context, memberName, milestone string) (string, error) {
	return fmt.Sprintf("ACHIEVED:%s:%s", memberName, milestone), nil
}

func (mockMilestoneFormatter) FormatMilestoneApproaching(ctx context.Context, memberName, milestone, remaining string) (string, error) {
	return fmt.Sprintf("APPROACHING:%s:%s:%s", memberName, milestone, remaining), nil
}

func TestSendMilestoneAlerts_SendsAndMarksBothKinds(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		unnotifiedMilestones: []ytstats.MilestoneNotification{
			{ChannelID: "UC1", MemberName: "A", Value: 100000},
		},
		unnotifiedApproaching: []ytstats.ApproachingNotification{
			{ChannelID: "UC2", MemberName: "B", MilestoneValue: 1000000, CurrentSubs: 990000},
		},
	}

	scheduler := &schedulerImpl{
		statsRepository: repository,
		formatter:       mockMilestoneFormatter{},
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	var sent []string
	var sentMu sync.Mutex
	sendMessage := func(room, message string) error {
		sentMu.Lock()
		defer sentMu.Unlock()
		sent = append(sent, room+"|"+message)
		return nil
	}

	rooms := []string{"room-1", "room-2"}
	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, rooms); err != nil {
		t.Fatalf("SendMilestoneAlerts() error = %v", err)
	}

	if len(sent) != 4 {
		t.Fatalf("sent messages = %d, want 4", len(sent))
	}
	if len(repository.markedMilestones) != 1 {
		t.Fatalf("marked milestones = %d, want 1", len(repository.markedMilestones))
	}
	if len(repository.markedApproaching) != 1 {
		t.Fatalf("marked approaching = %d, want 1", len(repository.markedApproaching))
	}
}

func TestSendMilestoneAlerts_DoesNotMarkWhenAllRoomSendsFail(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		unnotifiedMilestones: []ytstats.MilestoneNotification{
			{ChannelID: "UC1", MemberName: "A", Value: 100000},
		},
		unnotifiedApproaching: []ytstats.ApproachingNotification{
			{ChannelID: "UC2", MemberName: "B", MilestoneValue: 1000000, CurrentSubs: 990000},
		},
	}

	scheduler := &schedulerImpl{
		statsRepository: repository,
		formatter:       mockMilestoneFormatter{},
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	sendMessage := func(room, message string) error {
		return errors.New("send failed")
	}

	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, []string{"room-1", "room-2"}); err != nil {
		t.Fatalf("SendMilestoneAlerts() error = %v", err)
	}

	if len(repository.markedMilestones) != 0 {
		t.Fatalf("marked milestones = %d, want 0", len(repository.markedMilestones))
	}
	if len(repository.markedApproaching) != 0 {
		t.Fatalf("marked approaching = %d, want 0", len(repository.markedApproaching))
	}
}

func TestSendMilestoneAlerts_DoesNotMarkMilestoneWhenAnyRoomFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		unnotifiedMilestones: []ytstats.MilestoneNotification{
			{ChannelID: "UC1", MemberName: "A", Value: 100000},
		},
		unnotifiedApproaching: []ytstats.ApproachingNotification{
			{ChannelID: "UC2", MemberName: "B", MilestoneValue: 1000000, CurrentSubs: 990000},
		},
	}

	scheduler := &schedulerImpl{
		statsRepository: repository,
		formatter:       mockMilestoneFormatter{},
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	sendMessage := func(room, message string) error {
		switch {
		case room == "room-2" && message == "ACHIEVED:A:10만":
			return nil
		default:
			return errors.New("send failed")
		}
	}

	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, []string{"room-1", "room-2"}); err != nil {
		t.Fatalf("SendMilestoneAlerts() error = %v", err)
	}

	if len(repository.markedMilestones) != 0 {
		t.Fatalf("marked milestones = %d, want 0", len(repository.markedMilestones))
	}
	if len(repository.markedApproaching) != 0 {
		t.Fatalf("marked approaching = %d, want 0", len(repository.markedApproaching))
	}
}

func TestSendMilestoneAlerts_DoesNotMarkApproachingWhenAnyRoomFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		unnotifiedApproaching: []ytstats.ApproachingNotification{
			{ChannelID: "UC2", MemberName: "B", MilestoneValue: 1000000, CurrentSubs: 990000},
		},
	}

	scheduler := &schedulerImpl{
		statsRepository: repository,
		formatter:       mockMilestoneFormatter{},
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	sendMessage := func(room, message string) error {
		switch {
		case room == "room-2" && message == "APPROACHING:B:100만:1만":
			return nil
		default:
			return errors.New("send failed")
		}
	}

	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, []string{"room-1", "room-2"}); err != nil {
		t.Fatalf("SendMilestoneAlerts() error = %v", err)
	}

	if len(repository.markedMilestones) != 0 {
		t.Fatalf("marked milestones = %d, want 0", len(repository.markedMilestones))
	}
	if len(repository.markedApproaching) != 0 {
		t.Fatalf("marked approaching = %d, want 0", len(repository.markedApproaching))
	}
}

func TestSendMilestoneAlerts_RetryDoesNotResendToAlreadySucceededRooms(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		unnotifiedMilestones: []ytstats.MilestoneNotification{
			{ChannelID: "UC1", MemberName: "A", Value: 100000},
		},
	}

	scheduler := &schedulerImpl{
		statsRepository: repository,
		formatter:       mockMilestoneFormatter{},
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	rooms := []string{"room-1", "room-2"}

	var mu sync.Mutex
	sentByRoom := map[string]int{}
	failRoom1 := true
	sendMessage := func(room, message string) error {
		mu.Lock()
		defer mu.Unlock()
		if failRoom1 && room == "room-1" {
			return errors.New("send failed")
		}
		sentByRoom[room]++
		return nil
	}

	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, rooms); err != nil {
		t.Fatalf("first SendMilestoneAlerts() error = %v", err)
	}
	if len(repository.markedMilestones) != 0 {
		t.Fatalf("after partial failure marked milestones = %d, want 0", len(repository.markedMilestones))
	}
	if sentByRoom["room-2"] != 1 {
		t.Fatalf("room-2 sent count after first cycle = %d, want 1", sentByRoom["room-2"])
	}

	failRoom1 = false
	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, rooms); err != nil {
		t.Fatalf("second SendMilestoneAlerts() error = %v", err)
	}

	if sentByRoom["room-2"] != 1 {
		t.Fatalf("room-2 resent on retry: count = %d, want 1 (no duplicate)", sentByRoom["room-2"])
	}
	if sentByRoom["room-1"] != 1 {
		t.Fatalf("room-1 sent count after retry = %d, want 1", sentByRoom["room-1"])
	}
	if len(repository.markedMilestones) != 1 {
		t.Fatalf("after full success marked milestones = %d, want 1", len(repository.markedMilestones))
	}
}

func TestHB05PartialRetryDoesNotResendHealthyRooms_2411cf47(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		unnotifiedMilestones: []ytstats.MilestoneNotification{
			{ChannelID: "UC1", MemberName: "A", Value: 100000},
		},
	}
	scheduler := &schedulerImpl{
		statsRepository: repository,
		formatter:       mockMilestoneFormatter{},
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	rooms := []string{"room-1", "room-2"}
	var mu sync.Mutex
	sentByRoom := map[string]int{}
	failRoom1 := true
	sendMessage := func(room, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		if failRoom1 && room == "room-1" {
			return errors.New("send failed")
		}
		sentByRoom[room]++
		return nil
	}

	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, rooms); err != nil {
		t.Fatalf("first SendMilestoneAlerts() error = %v", err)
	}
	if sentByRoom["room-2"] != 1 {
		t.Fatalf("room-2 sent count after first cycle = %d, want 1", sentByRoom["room-2"])
	}

	failRoom1 = false
	if err := scheduler.SendMilestoneAlerts(context.Background(), sendMessage, rooms); err != nil {
		t.Fatalf("retry SendMilestoneAlerts() error = %v", err)
	}
	if sentByRoom["room-2"] != 1 {
		t.Fatalf("healthy room-2 resent on partial-failure retry: count = %d, want 1", sentByRoom["room-2"])
	}
	if sentByRoom["room-1"] != 1 {
		t.Fatalf("room-1 sent count after retry = %d, want 1", sentByRoom["room-1"])
	}
}

func TestTrackAllSubscribers_UsesSaveStatsBatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	members := &mockMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "UC1", Name: "A"},
			{ChannelID: "UC2", Name: "B"},
		},
	}

	youtubeService := &mockTrackAllSubscribersService{
		stats: map[string]*ChannelStats{
			"UC1": {SubscriberCount: 1100, VideoCount: 11, ViewCount: 10001},
			"UC2": {SubscriberCount: 2200, VideoCount: 22, ViewCount: 20002},
		},
	}

	repository := &mockTrackAllSubscribersRepository{
		latestByChannel: map[string]*domain.TimestampedStats{
			"UC1": {SubscriberCount: 1000, VideoCount: 10, ViewCount: 10000},
			"UC2": {SubscriberCount: 2000, VideoCount: 20, ViewCount: 20000},
		},
	}

	scheduler := &schedulerImpl{
		youtube:         youtubeService,
		statsRepository: repository,
		membersData:     members,
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	scheduler.trackAllSubscribers(context.Background())

	if repository.saveBatchCalls != 1 {
		t.Fatalf("saveBatchCalls = %d, want 1", repository.saveBatchCalls)
	}
	if repository.latestBatchCalls != 1 {
		t.Fatalf("latestBatchCalls = %d, want 1", repository.latestBatchCalls)
	}
	if repository.achievedCalls != 1 {
		t.Fatalf("achievedCalls = %d, want 1", repository.achievedCalls)
	}
	if repository.saveBatchRows != 2 {
		t.Fatalf("saveBatchRows = %d, want 2", repository.saveBatchRows)
	}
	if repository.saveSingleCalls != 0 {
		t.Fatalf("saveSingleCalls = %d, want 0", repository.saveSingleCalls)
	}
	if repository.recordChangeCalls != 2 {
		t.Fatalf("recordChangeCalls = %d, want 2", repository.recordChangeCalls)
	}
	if repository.hasAchievedCalls != 0 {
		t.Fatalf("hasAchievedCalls = %d, want 0", repository.hasAchievedCalls)
	}
}

func TestTrackAllSubscribers_SkipsChangeProcessingWhenBatchSaveFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	members := &mockMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "UC1", Name: "A"},
		},
	}

	youtubeService := &mockTrackAllSubscribersService{
		stats: map[string]*ChannelStats{
			"UC1": {SubscriberCount: 1100, VideoCount: 11, ViewCount: 10001},
		},
	}

	repository := &mockTrackAllSubscribersRepository{
		latestByChannel: map[string]*domain.TimestampedStats{
			"UC1": {SubscriberCount: 1000, VideoCount: 10, ViewCount: 10000},
		},
		saveBatchErr: errors.New("insert failure"),
	}

	scheduler := &schedulerImpl{
		youtube:         youtubeService,
		statsRepository: repository,
		membersData:     members,
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	scheduler.trackAllSubscribers(context.Background())

	if repository.saveBatchCalls != 1 {
		t.Fatalf("saveBatchCalls = %d, want 1", repository.saveBatchCalls)
	}
	if repository.latestBatchCalls != 1 {
		t.Fatalf("latestBatchCalls = %d, want 1", repository.latestBatchCalls)
	}
	if repository.saveSingleCalls != 0 {
		t.Fatalf("saveSingleCalls = %d, want 0", repository.saveSingleCalls)
	}
	if repository.recordChangeCalls != 0 {
		t.Fatalf("recordChangeCalls = %d, want 0", repository.recordChangeCalls)
	}
}

func TestTrackAllSubscribers_UsesHasAchievedFallbackWhenMilestonePreloadFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	members := &mockMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "UC1", Name: "A"},
		},
	}

	youtubeService := &mockTrackAllSubscribersService{
		stats: map[string]*ChannelStats{
			"UC1": {SubscriberCount: 101000, VideoCount: 11, ViewCount: 10001},
		},
	}

	repository := &mockTrackAllSubscribersRepository{
		latestByChannel: map[string]*domain.TimestampedStats{
			"UC1": {SubscriberCount: 99000, VideoCount: 10, ViewCount: 10000},
		},
		achievedErr: errors.New("preload failure"),
	}

	scheduler := &schedulerImpl{
		youtube:         youtubeService,
		statsRepository: repository,
		membersData:     members,
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	scheduler.trackAllSubscribers(context.Background())

	if repository.achievedCalls != 1 {
		t.Fatalf("achievedCalls = %d, want 1", repository.achievedCalls)
	}
	if repository.hasAchievedCalls != 1 {
		t.Fatalf("hasAchievedCalls = %d, want 1", repository.hasAchievedCalls)
	}
	if repository.saveMilestoneCalls != 1 {
		t.Fatalf("saveMilestoneCalls = %d, want 1", repository.saveMilestoneCalls)
	}
}

func TestProcessMilestones_FallsBackToHasAchievedWhenPreloadUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &mockTrackAllSubscribersRepository{
		hasAchievedResult: true,
	}

	scheduler := &schedulerImpl{
		statsRepository: repository,
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	member := &domain.Member{
		ChannelID: "UC1",
		Name:      "A",
	}

	achieved, checkErrors, saveErrors := scheduler.processMilestones(
		context.Background(),
		"UC1",
		member,
		[]uint64{100000},
		nil,
		false,
		time.Now(),
	)

	if achieved != 0 {
		t.Fatalf("achieved = %d, want 0", achieved)
	}
	if checkErrors != 0 {
		t.Fatalf("checkErrors = %d, want 0", checkErrors)
	}
	if saveErrors != 0 {
		t.Fatalf("saveErrors = %d, want 0", saveErrors)
	}
	if repository.hasAchievedCalls != 1 {
		t.Fatalf("hasAchievedCalls = %d, want 1", repository.hasAchievedCalls)
	}
	if repository.saveMilestoneCalls != 0 {
		t.Fatalf("saveMilestoneCalls = %d, want 0", repository.saveMilestoneCalls)
	}
}

type mockRecentVideosService struct {
	maxConcurrent int
	current       int
	mu            sync.Mutex
	sleep         time.Duration
}

func (m *mockRecentVideosService) SetScraperProxyEnabled(enabled bool) bool { return enabled }
func (m *mockRecentVideosService) ScraperProxyEnabled() bool                { return false }

func (m *mockRecentVideosService) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	return nil, nil
}

func (m *mockRecentVideosService) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error) {
	m.mu.Lock()
	m.current++
	if m.current > m.maxConcurrent {
		m.maxConcurrent = m.current
	}
	m.mu.Unlock()

	sleep := m.sleep
	if sleep == 0 {
		sleep = 10 * time.Millisecond
	}
	time.Sleep(sleep)

	m.mu.Lock()
	m.current--
	m.mu.Unlock()

	return []string{channelID + "-video"}, nil
}

func TestFetchRecentVideosRotation_UsesBoundedParallelism(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	youtubeService := &mockRecentVideosService{}
	cache := &cachemocks.Client{
		SetFunc: func(ctx context.Context, key string, value any, ttl time.Duration) error {
			return nil
		},
	}

	members := &mockMemberDataProvider{members: make([]*domain.Member, channelsPerBatch+5)}
	for i := range members.members {
		members.members[i] = &domain.Member{
			ChannelID: fmt.Sprintf("UC%d", i+1),
			Name:      fmt.Sprintf("M%d", i+1),
		}
	}

	scheduler := &schedulerImpl{
		youtube:     youtubeService,
		cache:       cache,
		membersData: members,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}

	scheduler.fetchRecentVideosRotation(context.Background(), 0)

	if youtubeService.maxConcurrent <= 1 {
		t.Fatalf("maxConcurrent = %d, want > 1", youtubeService.maxConcurrent)
	}
	if youtubeService.maxConcurrent > recentVideosFetchParallelism {
		t.Fatalf("maxConcurrent = %d, want <= %d", youtubeService.maxConcurrent, recentVideosFetchParallelism)
	}
}

func TestFetchRecentVideosRotation_CacheWritesUseBoundedParallelism(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	youtubeService := &mockRecentVideosService{
		sleep: time.Millisecond,
	}

	var (
		mu                 sync.Mutex
		cacheCurrent       int
		cacheMaxConcurrent int
	)
	cache := &cachemocks.Client{
		SetFunc: func(ctx context.Context, key string, value any, ttl time.Duration) error {
			mu.Lock()
			cacheCurrent++
			if cacheCurrent > cacheMaxConcurrent {
				cacheMaxConcurrent = cacheCurrent
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			cacheCurrent--
			mu.Unlock()
			return nil
		},
	}

	members := &mockMemberDataProvider{members: make([]*domain.Member, channelsPerBatch+5)}
	for i := range members.members {
		members.members[i] = &domain.Member{
			ChannelID: fmt.Sprintf("UC%d", i+1),
			Name:      fmt.Sprintf("M%d", i+1),
		}
	}

	scheduler := &schedulerImpl{
		youtube:     youtubeService,
		cache:       cache,
		membersData: members,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}

	scheduler.fetchRecentVideosRotation(context.Background(), 0)

	if cacheMaxConcurrent <= 1 {
		t.Fatalf("cacheMaxConcurrent = %d, want > 1", cacheMaxConcurrent)
	}
	if cacheMaxConcurrent > recentVideosFetchParallelism {
		t.Fatalf("cacheMaxConcurrent = %d, want <= %d", cacheMaxConcurrent, recentVideosFetchParallelism)
	}
}

type mockBatchOverlapGuardService struct {
	mu          sync.Mutex
	recentCalls int
	startedCh   chan struct{}
	releaseCh   chan struct{}
}

func (m *mockBatchOverlapGuardService) SetScraperProxyEnabled(enabled bool) bool { return enabled }
func (m *mockBatchOverlapGuardService) ScraperProxyEnabled() bool                { return false }

func (m *mockBatchOverlapGuardService) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	return map[string]*ChannelStats{}, nil
}

func (m *mockBatchOverlapGuardService) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error) {
	m.mu.Lock()
	m.recentCalls++
	m.mu.Unlock()

	select {
	case m.startedCh <- struct{}{}:
	default:
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.releaseCh:
		return []string{channelID + "-video"}, nil
	}
}

func (m *mockBatchOverlapGuardService) RecentCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recentCalls
}

func TestRunBatch_SkipsOverlapWhilePreviousBatchRunning(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	youtubeService := &mockBatchOverlapGuardService{
		startedCh: make(chan struct{}, recentVideosFetchParallelism),
		releaseCh: make(chan struct{}),
	}
	cache := &cachemocks.Client{
		SetFunc: func(ctx context.Context, key string, value any, ttl time.Duration) error {
			return nil
		},
	}
	members := &mockMemberDataProvider{
		members: make([]*domain.Member, channelsPerBatch),
	}
	for i := range members.members {
		members.members[i] = &domain.Member{
			ChannelID: fmt.Sprintf("UC%d", i+1),
			Name:      fmt.Sprintf("M%d", i+1),
		}
	}

	scheduler := &schedulerImpl{
		youtube:         youtubeService,
		statsRepository: &mockTrackAllSubscribersRepository{latestByChannel: map[string]*domain.TimestampedStats{}},
		cache:           cache,
		membersData:     members,
		logger:          logger,
		stopCh:          make(chan struct{}),
	}

	ctx := t.Context()
	firstDone := make(chan struct{})
	go func() {
		scheduler.runBatch(ctx)
		close(firstDone)
	}()

	for range recentVideosFetchParallelism {
		select {
		case <-youtubeService.startedCh:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for first batch to fill parallelism slots")
		}
	}

	scheduler.runBatch(ctx)

	if got := youtubeService.RecentCalls(); got != recentVideosFetchParallelism {
		t.Fatalf("recentCalls = %d, want %d while overlap guard is active", got, recentVideosFetchParallelism)
	}

	scheduler.batchMu.Lock()
	currentBatch := scheduler.currentBatch
	scheduler.batchMu.Unlock()
	if currentBatch != 1 {
		t.Fatalf("currentBatch = %d, want 1 after skipped overlapping batch", currentBatch)
	}

	close(youtubeService.releaseCh)

	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first batch to finish")
	}

	scheduler.runBatch(ctx)

	if got := youtubeService.RecentCalls(); got != channelsPerBatch*2 {
		t.Fatalf("recentCalls = %d, want %d after guard is released", got, channelsPerBatch*2)
	}

	scheduler.batchMu.Lock()
	currentBatch = scheduler.currentBatch
	scheduler.batchMu.Unlock()
	if currentBatch != 0 {
		t.Fatalf("currentBatch = %d, want 0 after next successful batch", currentBatch)
	}
}

func TestFinalizeNearMilestoneChannelMap_KeepsPartialResultsOnError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	partial := map[string]*domain.Channel{
		"UC1": {ID: "UC1"},
	}

	got := finalizeNearMilestoneChannelMap(logger, 2, partial, errors.New("batch failure"))
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got["UC1"] == nil || got["UC1"].ID != "UC1" {
		t.Fatalf("expected partial result for UC1 to be preserved, got %#v", got["UC1"])
	}
}

func TestFinalizeNearMilestoneChannelMap_InitializesNilMapOnError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	got := finalizeNearMilestoneChannelMap(logger, 2, nil, errors.New("batch failure"))
	if got == nil {
		t.Fatal("expected non-nil map")
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}
