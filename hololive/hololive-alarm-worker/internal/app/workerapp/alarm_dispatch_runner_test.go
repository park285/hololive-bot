package workerapp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAlarmDispatchRunnerTestSend = errors.New("send failed")

type alarmDispatchRunnerTestConsumer struct {
	batches           [][]domain.AlarmQueueEnvelope
	drainErr          error
	onDrain           func()
	markSending       []domain.AlarmQueueEnvelope
	markDispatched    []domain.AlarmQueueEnvelope
	quarantined       []domain.AlarmQueueEnvelope
	quarantineReason  string
	scheduledRetry    []domain.AlarmQueueEnvelope
	movedDLQ          []domain.AlarmQueueEnvelope
	requeued          []domain.AlarmQueueEnvelope
	releasedClaims    []string
	markDispatchedErr error
	quarantineErr     error
	scheduleRetryErr  error
	moveDLQErr        error
}

func (c *alarmDispatchRunnerTestConsumer) DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
	if c.onDrain != nil {
		c.onDrain()
	}
	if c.drainErr != nil {
		return nil, c.drainErr
	}
	if len(c.batches) == 0 {
		return nil, nil
	}
	batch := c.batches[0]
	c.batches = c.batches[1:]
	return batch, nil
}

func (c *alarmDispatchRunnerTestConsumer) MarkSending(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSending = append(c.markSending, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	return c.markDispatchedErr
}

func (c *alarmDispatchRunnerTestConsumer) Quarantine(_ context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error {
	c.quarantined = append(c.quarantined, envelopes...)
	c.quarantineReason = reason
	return c.quarantineErr
}

func (c *alarmDispatchRunnerTestConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.releasedClaims = append(c.releasedClaims, claimKeys...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) ScheduleRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, envelopes...)
	return c.scheduleRetryErr
}

func (c *alarmDispatchRunnerTestConsumer) MoveToDLQ(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.movedDLQ = append(c.movedDLQ, envelopes...)
	return c.moveDLQErr
}

func (c *alarmDispatchRunnerTestConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeued = append(c.requeued, envelopes...)
	return nil
}

type alarmDispatchRunnerTestSender struct {
	fail             bool
	karingErr        error
	roomID           string
	messages         []string
	clientRequestIDs []string
	karingRoomID     string
	karingRequests   []iris.KaringContentListRequest
}

func (s *alarmDispatchRunnerTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.roomID = roomID
	s.messages = append(s.messages, message)
	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}
	return nil
}

func (s *alarmDispatchRunnerTestSender) SendMessageWithClientRequestID(_ context.Context, roomID, message, clientRequestID string) error {
	s.roomID = roomID
	s.messages = append(s.messages, message)
	s.clientRequestIDs = append(s.clientRequestIDs, clientRequestID)
	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}
	return nil
}

func (s *alarmDispatchRunnerTestSender) SendKaringContentList(_ context.Context, roomID string, req *iris.KaringContentListRequest) error {
	s.karingRoomID = roomID
	if req != nil {
		s.karingRequests = append(s.karingRequests, *req)
	}
	if s.karingErr != nil {
		return s.karingErr
	}
	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}
	return nil
}

func TestAlarmDispatchRunnerRunOnceSendsKaringContentListRequest(t *testing.T) {
	start := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	thumbnail := "https://i.ytimg.com/vi/stream-1/maxresdefault.jpg"
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.Notification.Stream.ChannelName = "Test Channel"
	envelope.Notification.Stream.StartActual = &start
	envelope.Notification.Stream.Thumbnail = &thumbnail
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, "room-1", sender.karingRoomID)
	require.Len(t, sender.karingRequests, 1)
	req := sender.karingRequests[0]
	require.NotNil(t, req.ClientRequestID)
	assert.Contains(t, *req.ClientRequestID, "hololive-alarm:")
	assert.Equal(t, "room-1", req.ReceiverName)
	assert.Equal(t, int64(133266), req.TemplateID)
	assert.Equal(t, "라이브 시작", req.ExtraArgs["alarm_title"])
	assert.Equal(t, "지금 시작", req.ExtraArgs["time_left"])
	require.Len(t, req.Items, 1)
	item := req.Items[0]
	assert.Equal(t, "Test Stream", item.Title)
	assert.Equal(t, "https://youtube.com/watch?v=stream-1", item.URL)
	assert.Equal(t, "Test Member", item.MemberName)
	assert.Equal(t, "Test Channel", item.ChannelName)
	assert.Empty(t, item.Status)
	assert.Equal(t, "05/16 21:00", item.StartAt)
	assert.Equal(t, thumbnail, item.ThumbnailURL)
	assert.Equal(t, "youtube", item.Platform)
}

func TestAlarmDispatchRunnerUpcomingKaringRequestPreservesMinuteWindow(t *testing.T) {
	start := time.Date(2026, 5, 16, 12, 10, 0, 0, time.UTC)
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.Notification.MinutesUntil = 10
	envelope.Notification.Stream.Status = domain.StreamStatusUpcoming
	envelope.Notification.Stream.StartScheduled = &start
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.karingRequests, 1)
	req := sender.karingRequests[0]
	assert.Equal(t, int64(133266), req.TemplateID)
	assert.Equal(t, "방송 10분 전 알림", req.ExtraArgs["alarm_title"])
	assert.Equal(t, "10분 후 시작", req.ExtraArgs["time_left"])
	require.Len(t, req.Items, 1)
	item := req.Items[0]
	assert.Empty(t, item.Status)
	assert.Equal(t, "05/16 21:10", item.StartAt)
}

func TestAlarmDispatchRunnerKaringRequestPreservesConfiguredNickname(t *testing.T) {
	englishName := "Yuuki Sakuna"
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.Notification.Channel.Name = "사쿠나"
	envelope.Notification.Channel.EnglishName = &englishName
	envelope.Notification.Stream.Channel = envelope.Notification.Channel
	envelope.Notification.Stream.ChannelName = "사쿠나"

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, alarmDispatchGroup{
		roomID:        "room-1",
		notifications: []domain.AlarmNotification{envelope.Notification},
	})

	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Len(t, requests[0].Items, 1)
	assert.Equal(t, "사쿠나", requests[0].Items[0].MemberName)
	assert.Equal(t, "사쿠나", requests[0].Items[0].ChannelName)
}

func TestAlarmDispatchRunnerYouTubeOutboxCommunitySendsKaringRequest(t *testing.T) {
	publishedAt := time.Date(2026, 5, 16, 10, 30, 0, 0, time.UTC)
	thumbnailURL := "https://yt3.ggpht.com/community-image=s800"
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UCtest",
		MemberName: "Community Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "UgkxPost",
			Payload:   `{"post_id":"UgkxPost","content_text":"／\n\n새 커뮤니티 공지입니다\n두번째줄\n＼","images":[{"url":"https://yt3.ggpht.com/community-image=s288","width":288,"height":288},{"url":"` + thumbnailURL + `","width":800,"height":800}],"published_at":"` + publishedAt.Format(time.RFC3339Nano) + `"}`,
		}},
	}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, "room-1", sender.karingRoomID)
	require.Len(t, sender.karingRequests, 1)
	req := sender.karingRequests[0]
	assert.Equal(t, "room-1", req.ReceiverName)
	assert.Equal(t, int64(133266), req.TemplateID)
	assert.Equal(t, "커뮤니티 알림", req.ExtraArgs["alarm_title"])
	assert.Equal(t, "새 커뮤니티", req.ExtraArgs["time_left"])
	require.Len(t, req.Items, 1)
	item := req.Items[0]
	assert.Equal(t, "새 커뮤니티 공지입니다 두번째줄", item.Title)
	assert.Equal(t, "https://www.youtube.com/post/UgkxPost", item.URL)
	assert.Equal(t, "Community Member", item.MemberName)
	assert.Equal(t, "Community Member", item.ChannelName)
	assert.Equal(t, "커뮤니티", string(item.Status))
	assert.Equal(t, "05/16 19:30", item.StartAt)
	assert.Equal(t, thumbnailURL, item.ThumbnailURL)
	assert.Equal(t, "youtube", item.Platform)
}

func TestAlarmDispatchRunnerYouTubeOutboxMilestoneUsesTextDispatchWhenKaringEnabled_f8d2b5af(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindMilestone,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UCtest",
		MemberName: "Milestone Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "milestone-1",
			Payload:   `{"milestone":"100만"}`,
		}},
	}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newCelebrationTestRenderer(t), karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, sender.karingRequests)
	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.messages[0], "100만")
	assert.Len(t, consumer.markDispatched, 1)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerYouTubeOutboxCommunityNormalizesLiteralNewlines(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  "UCtest",
		MemberName: "Community Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "UgkxPost",
			Payload:   `{"post_id":"UgkxPost","content_text":"webpicker community smoke 134905\\nthumbnail/text render check"}`,
		}},
	}

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, alarmDispatchGroup{
		roomID:    "room-1",
		envelopes: []domain.AlarmQueueEnvelope{envelope},
	})

	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "커뮤니티 알림", requests[0].ExtraArgs["alarm_title"])
	require.Len(t, requests[0].Items, 1)
	assert.Equal(t, "webpicker community smoke 134905 thumbnail/text render check", requests[0].Items[0].Title)
}

func TestAlarmDispatchRunnerYouTubeOutboxContentKindsPreserveLabels(t *testing.T) {
	testCases := []struct {
		name          string
		kind          domain.OutboxKind
		payload       string
		wantTitle     string
		wantStatus    string
		wantAlarm     string
		wantTimeLeft  string
		wantURL       string
		wantThumbnail string
	}{
		{
			name:          "new video",
			kind:          domain.OutboxKindNewVideo,
			payload:       `{"video_id":"video000001","title":"새 영상 제목","thumbnail":[{"url":"https://i.ytimg.com/vi/video000001/mqdefault.jpg","width":320,"height":180},{"url":"https://i.ytimg.com/vi/video000001/maxresdefault.jpg","width":1280,"height":720}]}`,
			wantTitle:     "새 영상 제목",
			wantStatus:    "새 영상",
			wantAlarm:     "새 영상",
			wantTimeLeft:  "새 영상",
			wantURL:       "https://youtube.com/watch?v=video000001",
			wantThumbnail: "https://i.ytimg.com/vi/video000001/maxresdefault.jpg",
		},
		{
			name:          "new short",
			kind:          domain.OutboxKindNewShort,
			payload:       `{"video_id":"short000001","title":"쇼츠 제목","thumbnail":[{"url":"//i.ytimg.com/vi/short000001/hqdefault.jpg","width":480,"height":360}]}`,
			wantTitle:     "쇼츠 제목",
			wantStatus:    "쇼츠",
			wantAlarm:     "쇼츠 알림",
			wantTimeLeft:  "새 쇼츠",
			wantURL:       "https://www.youtube.com/shorts/short000001",
			wantThumbnail: "https://i.ytimg.com/vi/short000001/hqdefault.jpg",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
			envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
			envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
				Kind:       tc.kind,
				AlarmType:  tc.kind.ToAlarmType(),
				ChannelID:  "UCtest",
				MemberName: "Content Member",
				Items: []domain.YouTubeOutboxItem{{
					OutboxID:  1,
					ContentID: "content-1",
					Payload:   tc.payload,
				}},
			}
			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := alarmDispatchRunner{consumer: consumer, sender: sender, karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			require.Len(t, sender.karingRequests, 1)
			req := sender.karingRequests[0]
			assert.Equal(t, int64(133266), req.TemplateID)
			assert.Equal(t, tc.wantAlarm, req.ExtraArgs["alarm_title"])
			assert.Equal(t, tc.wantTimeLeft, req.ExtraArgs["time_left"])
			require.Len(t, req.Items, 1)
			item := req.Items[0]
			assert.Equal(t, tc.wantTitle, item.Title)
			assert.Equal(t, tc.wantStatus, string(item.Status))
			assert.Equal(t, tc.wantURL, item.URL)
			assert.Equal(t, tc.wantThumbnail, item.ThumbnailURL)
		})
	}
}

func TestAlarmDispatchRunnerKaringChunksRequestsByFour(t *testing.T) {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, 5)
	for i := range 5 {
		envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
		envelope.Notification.Channel.Name = fmt.Sprintf("Member %d", i+1)
		envelope.Notification.Stream.ID = fmt.Sprintf("stream-%d", i+1)
		envelope.Notification.Stream.Title = fmt.Sprintf("Stream %d", i+1)
		envelopes = append(envelopes, envelope)
	}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{envelopes}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.karingRequests, 2)
	assert.Equal(t, int64(133267), sender.karingRequests[0].TemplateID)
	assert.Len(t, sender.karingRequests[0].Items, 4)
	assert.Equal(t, "Stream 1", sender.karingRequests[0].Items[0].Title)
	assert.Equal(t, "Stream 4", sender.karingRequests[0].Items[3].Title)
	assert.Equal(t, int64(133266), sender.karingRequests[1].TemplateID)
	require.Len(t, sender.karingRequests[1].Items, 1)
	assert.Equal(t, "Stream 5", sender.karingRequests[1].Items[0].Title)
	assert.Len(t, consumer.markSending, 5)
	assert.Len(t, consumer.markDispatched, 5)
}

func TestAlarmDispatchKaringRequestChunkTemplatesByItemCount(t *testing.T) {
	testCases := []struct {
		name          string
		itemCount     int
		wantTemplates []int64
		wantItemCount []int
	}{
		{name: "one", itemCount: 1, wantTemplates: []int64{133266}, wantItemCount: []int{1}},
		{name: "two", itemCount: 2, wantTemplates: []int64{133223}, wantItemCount: []int{2}},
		{name: "three", itemCount: 3, wantTemplates: []int64{133222}, wantItemCount: []int{3}},
		{name: "four", itemCount: 4, wantTemplates: []int64{133267}, wantItemCount: []int{4}},
		{name: "five", itemCount: 5, wantTemplates: []int64{133267, 133266}, wantItemCount: []int{4, 1}},
		{name: "six", itemCount: 6, wantTemplates: []int64{133267, 133223}, wantItemCount: []int{4, 2}},
		{name: "seven", itemCount: 7, wantTemplates: []int64{133267, 133222}, wantItemCount: []int{4, 3}},
		{name: "eight", itemCount: 8, wantTemplates: []int64{133267, 133267}, wantItemCount: []int{4, 4}},
		{name: "nine", itemCount: 9, wantTemplates: []int64{133267, 133267, 133266}, wantItemCount: []int{4, 4, 1}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			envelopes := make([]domain.AlarmQueueEnvelope, 0, tc.itemCount)
			for i := range tc.itemCount {
				envelope := alarmDispatchRunnerTestEnvelope("464252100463241", nil)
				envelope.Notification.Channel.Name = fmt.Sprintf("Member %d", i+1)
				envelope.Notification.Stream.ID = fmt.Sprintf("stream-%d", i+1)
				envelope.Notification.Stream.Title = fmt.Sprintf("Stream %d", i+1)
				envelopes = append(envelopes, envelope)
			}
			groups := groupAlarmDispatchEnvelopes(envelopes)
			require.Len(t, groups, 1)

			requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, groups[0])

			require.NoError(t, err)
			require.Len(t, requests, len(tc.wantTemplates))
			for i, request := range requests {
				assert.Equal(t, tc.wantTemplates[i], request.TemplateID)
				assert.Len(t, request.Items, tc.wantItemCount[i])
				assert.Equal(t, int64(464252100463241), request.ReceiverRoomID)
				assert.Zero(t, request.ReceiverName)
			}
		})
	}
}

func TestAlarmDispatchKaringRequestUsesReceiverRoomID(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("464252100463241", nil)
	group := newAlarmDispatchGroup(&envelope)

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, group)

	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Zero(t, requests[0].ReceiverName)
	assert.Equal(t, int64(464252100463241), requests[0].ReceiverRoomID)
}

func TestAlarmDispatchKaringTemplateIDByItemCount(t *testing.T) {
	assert.Equal(t, int64(133266), alarmDispatchKaringTemplateID(1))
	assert.Equal(t, int64(133223), alarmDispatchKaringTemplateID(2))
	assert.Equal(t, int64(133222), alarmDispatchKaringTemplateID(3))
	assert.Equal(t, int64(133267), alarmDispatchKaringTemplateID(4))
	assert.Zero(t, alarmDispatchKaringTemplateID(5))
}

func TestAlarmDispatchRunnerRunOnceSendsAndMarksDispatched(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, "room-1", sender.roomID)
	require.Len(t, sender.messages, 1)
	require.Len(t, sender.clientRequestIDs, 1)
	assert.Contains(t, sender.clientRequestIDs[0], "hololive-alarm:")
	assert.Contains(t, sender.messages[0], "방송 시작")
	assert.Empty(t, sender.karingRequests)
	assert.Len(t, consumer.markSending, 1)
	assert.Len(t, consumer.markDispatched, 1)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
}

func TestParseAlarmDispatchKaringEnabledFromEnv(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "")
	assert.False(t, parseAlarmDispatchKaringEnabled())

	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	assert.True(t, parseAlarmDispatchKaringEnabled())

	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	assert.False(t, parseAlarmDispatchKaringEnabled())
}

func TestAlarmDispatchRunnerRunOnceSchedulesRetryOnSendFailure(t *testing.T) {
	consumer := &alarmDispatchRunnerLegacyTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	require.NotNil(t, consumer.scheduledRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledRetry[0].Retry.Attempt)
	assert.Contains(t, consumer.scheduledRetry[0].Retry.LastError, errAlarmDispatchRunnerTestSend.Error())
	assert.Empty(t, consumer.markDispatched)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerQuarantinesPGSendFailureAfterMarkSending(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.quarantined, 1)
	assert.Contains(t, consumer.quarantineReason, errAlarmDispatchRunnerTestSend.Error())
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerRetriesKaringBadGatewayAfterMarkSending(t *testing.T) {
	// alarmDispatchRunnerTestConsumer는 alarmDispatchSendingRetryConsumer를 구현하지 않으므로
	// persistSendingRetry가 persistPreSendFailure로 폴백하여 ScheduleRetry를 호출한다.
	karingErr := fmt.Errorf("iris send karing content list: %w", &iris.HTTPError{StatusCode: 502, URL: "/karing/content-list"})
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: karingErr}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.scheduledRetry, 1)
	require.NotNil(t, consumer.scheduledRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledRetry[0].Retry.Attempt)
	assert.Contains(t, consumer.scheduledRetry[0].Retry.LastError, "returned 502")
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerReturnsErrorWhenPostSendQuarantineFails(t *testing.T) {
	quarantineErr := errors.New("quarantine failed")
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:       [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		quarantineErr: quarantineErr,
	}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	assert.ErrorIs(t, err, quarantineErr)
	assert.Empty(t, consumer.scheduledRetry)
}

func TestAlarmDispatchRunnerRetriesRenderFailureBeforeMarkSending(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = nil
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	assert.Empty(t, consumer.markSending)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, sender.messages)
	assert.Empty(t, sender.karingRequests)
}

func TestAlarmDispatchRunnerDoesNotRetryMarkDispatchedFailureAfterSend(t *testing.T) {
	markErr := errors.New("mark dispatched failed")
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:           [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		markDispatchedErr: markErr,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	assert.ErrorIs(t, err, markErr)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.quarantined)
}

func TestAlarmDispatchRunnerUsesLegacyRetryWhenConsumerCannotQuarantine(t *testing.T) {
	consumer := &alarmDispatchRunnerLegacyTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerRunOnceMovesExhaustedRetryToDLQAndReleasesClaims(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", &domain.AlarmQueueRetryMetadata{Attempt: 2})
	envelope.ClaimKeys = []string{"alarm:dispatch:claim:room-1:stream-1"}
	consumer := &alarmDispatchRunnerLegacyTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.scheduledRetry)
	require.Len(t, consumer.movedDLQ, 1)
	require.NotNil(t, consumer.movedDLQ[0].Retry)
	assert.Equal(t, 3, consumer.movedDLQ[0].Retry.Attempt)
	assert.Equal(t, []string{"alarm:dispatch:claim:room-1:stream-1"}, consumer.releasedClaims)
}

func TestAlarmDispatchRunnerWaitsOnIdleWaiterForEmptyPGBatch(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: false}
	runner := alarmDispatchRunner{consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.False(t, keepGoing)
	assert.Equal(t, 1, waiter.waits)
	assert.Zero(t, waiter.resets)
}

func TestAlarmDispatchRunnerResetsIdleWaiterAfterProcessedBatch(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.True(t, keepGoing)
	assert.Zero(t, waiter.waits)
	assert.Equal(t, 1, waiter.resets)
}

func TestAlarmDispatchRunnerYieldsAfterMaxBatchesPerWake(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{
		{alarmDispatchRunnerTestEnvelope("room-1", nil)},
		{alarmDispatchRunnerTestEnvelope("room-1", nil)},
	}}
	yieldCount := 0
	runner := alarmDispatchRunner{
		consumer:          consumer,
		sender:            &alarmDispatchRunnerTestSender{},
		renderer:          newAlarmDispatchTestRenderer(t),
		maxBatch:          10,
		maxBatchesPerWake: 2,
		yield: func(context.Context) bool {
			yieldCount++
			return true
		},
	}

	assert.True(t, runner.runStep(t.Context()))
	assert.Zero(t, yieldCount)
	assert.True(t, runner.runStep(t.Context()))
	assert.Equal(t, 1, yieldCount)
}

func TestAlarmDispatchRunnerStartProcessesBatchesUntilIdleWaitStops(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{
		{alarmDispatchRunnerTestEnvelope("room-1", nil)},
		{alarmDispatchRunnerTestEnvelope("room-2", nil)},
	}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: false}
	sender := &alarmDispatchRunnerTestSender{}
	runner := &alarmDispatchRunner{
		consumer:   consumer,
		sender:     sender,
		renderer:   newAlarmDispatchTestRenderer(t),
		maxBatch:   10,
		idleWaiter: waiter,
	}

	err := runner.Start(t.Context())

	require.NoError(t, err)
	require.Len(t, consumer.markDispatched, 2)
	assert.Equal(t, []string{"room-1", "room-2"}, []string{consumer.markDispatched[0].Notification.RoomID, consumer.markDispatched[1].Notification.RoomID})
	assert.Len(t, sender.messages, 2)
	assert.Equal(t, 2, waiter.resets)
	assert.Equal(t, 1, waiter.waits)
	assert.Zero(t, runner.batchesSinceWake)
}

func TestAlarmDispatchRunnerRunStepStopsWhenDrainErrorArrivesAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	consumer := &alarmDispatchRunnerTestConsumer{
		drainErr: errors.New("drain failed"),
		onDrain:  cancel,
	}
	runner := alarmDispatchRunner{
		consumer: consumer,
		sender:   &alarmDispatchRunnerTestSender{},
		maxBatch: 10,
	}

	keepGoing := runner.runStep(ctx)

	assert.False(t, keepGoing)
	assert.Empty(t, consumer.markDispatched)
}

func TestGroupAlarmDispatchEnvelopesSeparatesScheduledMinuteBuckets(t *testing.T) {
	firstStart := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)
	first := alarmDispatchRunnerTestEnvelope("room-1", nil)
	second := alarmDispatchRunnerTestEnvelope("room-1", nil)
	first.Notification.Stream.StartScheduled = &firstStart
	second.Notification.Stream.StartScheduled = &secondStart

	groups := groupAlarmDispatchEnvelopes([]domain.AlarmQueueEnvelope{first, second})

	assert.Len(t, groups, 2)
}

func TestGroupAlarmDispatchEnvelopesForKaringCollapsesScheduledMinuteBuckets(t *testing.T) {
	firstStart := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)
	first := alarmDispatchRunnerTestEnvelope("room-1", nil)
	second := alarmDispatchRunnerTestEnvelope("room-1", nil)
	first.Notification.MinutesUntil = 5
	second.Notification.MinutesUntil = 5
	first.Notification.Stream.StartScheduled = &firstStart
	second.Notification.Stream.StartScheduled = &secondStart

	groups := groupAlarmDispatchEnvelopesForKaring([]domain.AlarmQueueEnvelope{first, second}, true)

	require.Len(t, groups, 1)
	assert.Len(t, groups[0].envelopes, 2)
}

func TestRenderAlarmDispatchNotificationGroupMatchesLegacyValkeyRenderer(t *testing.T) {
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope("room-1", nil)
	second := alarmDispatchRunnerTestEnvelope("room-1", nil)
	first.Notification.MinutesUntil = 3
	second.Notification.MinutesUntil = 1
	first.Notification.Channel.Name = "Member1"
	second.Notification.Channel.Name = "Member2"
	first.Notification.Stream.ID = "abc"
	second.Notification.Stream.ID = "def"
	first.Notification.Stream.Title = "Title1"
	second.Notification.Stream.Title = "Title2"
	first.Notification.Stream.StartScheduled = &start
	second.Notification.Stream.StartScheduled = &start
	group := groupAlarmDispatchEnvelopes([]domain.AlarmQueueEnvelope{first, second})[0]

	message, err := renderAlarmDispatchGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, group)

	require.NoError(t, err)
	assert.Equal(t, "⏰ 방송 1분 전\n\n"+
		"⏰ Member1 방송 3분 전\n  Title1\n  https://youtube.com/watch?v=abc\n\n"+
		"⏰ Member2 방송 예정\n  Title2\n  https://youtube.com/watch?v=def", message)
}

func TestRenderAlarmDispatchNotificationLiveCatchupUsesRecoveredUpcomingMessage(t *testing.T) {
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	notification := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	notification.MinutesUntil = 5
	notification.Channel.Name = "Member"
	notification.Stream.ID = "live-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.StartScheduled = &start
	notification.Stream.StartActual = &start

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"⏰ Member 방송 5분 전\n  Live Title\n  https://youtube.com/watch?v=live-1",
		got,
	)
}

func TestResolveAlarmDispatchURLFallsBackLikeLegacyValkeyRenderer(t *testing.T) {
	twitchOnlyWithoutURL := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	twitchOnlyWithoutURL.Stream.IsTwitchOnly = true

	chzzkOnlyWithoutURL := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	chzzkOnlyWithoutURL.Stream.IsChzzkOnly = true

	assert.Equal(t, "https://youtube.com/watch?v=stream-1", resolveAlarmDispatchURL(&twitchOnlyWithoutURL))
	assert.Equal(t, "https://youtube.com/watch?v=stream-1", resolveAlarmDispatchURL(&chzzkOnlyWithoutURL))
}

func alarmDispatchRunnerTestEnvelope(roomID string, retry *domain.AlarmQueueRetryMetadata) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       roomID,
			MinutesUntil: 0,
			Channel:      &domain.Channel{Name: "Test Member"},
			Stream: &domain.Stream{
				ID:    "stream-1",
				Title: "Test Stream",
			},
		},
		Retry: retry,
	}
}

type alarmDispatchRunnerLegacyTestConsumer struct {
	batches           [][]domain.AlarmQueueEnvelope
	markSending       []domain.AlarmQueueEnvelope
	markDispatched    []domain.AlarmQueueEnvelope
	scheduledRetry    []domain.AlarmQueueEnvelope
	movedDLQ          []domain.AlarmQueueEnvelope
	requeued          []domain.AlarmQueueEnvelope
	releasedClaims    []string
	markDispatchedErr error
	scheduleRetryErr  error
	moveDLQErr        error
}

func (c *alarmDispatchRunnerLegacyTestConsumer) DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
	if len(c.batches) == 0 {
		return nil, nil
	}
	batch := c.batches[0]
	c.batches = c.batches[1:]
	return batch, nil
}

func (c *alarmDispatchRunnerLegacyTestConsumer) MarkSending(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSending = append(c.markSending, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerLegacyTestConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	return c.markDispatchedErr
}

func (c *alarmDispatchRunnerLegacyTestConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.releasedClaims = append(c.releasedClaims, claimKeys...)
	return nil
}

func (c *alarmDispatchRunnerLegacyTestConsumer) ScheduleRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, envelopes...)
	return c.scheduleRetryErr
}

func (c *alarmDispatchRunnerLegacyTestConsumer) MoveToDLQ(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.movedDLQ = append(c.movedDLQ, envelopes...)
	return c.moveDLQErr
}

func (c *alarmDispatchRunnerLegacyTestConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeued = append(c.requeued, envelopes...)
	return nil
}

type alarmDispatchRunnerTestIdleWaiter struct {
	waits       int
	resets      int
	returnValue bool
}

func (w *alarmDispatchRunnerTestIdleWaiter) Wait(context.Context) bool {
	w.waits++
	return w.returnValue
}

func (w *alarmDispatchRunnerTestIdleWaiter) Reset() {
	w.resets++
}
