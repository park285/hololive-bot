package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/util"
)

var errAlarmDispatchRunnerTestSend = errors.New("send failed")

type alarmDispatchRunnerTestConsumer struct {
	batches               [][]domain.AlarmQueueEnvelope
	drainErr              error
	onDrain               func()
	markSending           []domain.AlarmQueueEnvelope
	markDispatched        []domain.AlarmQueueEnvelope
	quarantined           []domain.AlarmQueueEnvelope
	quarantineReason      string
	scheduledRetry        []domain.AlarmQueueEnvelope
	scheduledSendingRetry []domain.AlarmQueueEnvelope
	preSendRequeued       []domain.AlarmQueueEnvelope
	movedDLQ              []domain.AlarmQueueEnvelope
	requeued              []domain.AlarmQueueEnvelope
	releasedClaims        []string
	markSendingErr        error
	markDispatchedErr     error
	quarantineErr         error
	routeFailuresErr      error
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
	return c.markSendingErr
}

func (c *alarmDispatchRunnerTestConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	return c.markDispatchedErr
}

func (c *alarmDispatchRunnerTestConsumer) Quarantine(_ context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	c.quarantined = append(c.quarantined, envelopes...)

	if cause != nil {
		c.quarantineReason = cause.Error()
	}

	return c.quarantineErr
}

func (c *alarmDispatchRunnerTestConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.releasedClaims = append(c.releasedClaims, claimKeys...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) RouteFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, retryEnvelopes...)
	c.movedDLQ = append(c.movedDLQ, dlqEnvelopes...)

	return c.routeFailuresErr
}

func (c *alarmDispatchRunnerTestConsumer) RouteSendingFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledSendingRetry = append(c.scheduledSendingRetry, retryEnvelopes...)
	c.movedDLQ = append(c.movedDLQ, dlqEnvelopes...)

	return nil
}

func (c *alarmDispatchRunnerTestConsumer) RequeuePreSend(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.preSendRequeued = append(c.preSendRequeued, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeued = append(c.requeued, envelopes...)
	return nil
}

type alarmDispatchRunnerTestSender struct {
	fail             bool
	messageErr       error
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

	if s.messageErr != nil {
		return s.messageErr
	}

	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}

	return nil
}

func (s *alarmDispatchRunnerTestSender) SendMessageWithClientRequestID(_ context.Context, roomID, message, clientRequestID string) error {
	s.roomID = roomID
	s.messages = append(s.messages, message)
	s.clientRequestIDs = append(s.clientRequestIDs, clientRequestID)

	if s.messageErr != nil {
		return s.messageErr
	}

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
	start := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	thumbnail := "https://i.ytimg.com/vi/stream-1/maxresdefault.jpg"
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.Stream.ChannelName = "Test Channel"
	envelope.Notification.Stream.StartActual = &start
	envelope.Notification.Stream.Thumbnail = &thumbnail

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, testAlarmRoomID, sender.karingRoomID)
	require.Len(t, sender.karingRequests, 1)

	req := sender.karingRequests[0]
	require.NotNil(t, req.ClientRequestID)
	assert.Contains(t, *req.ClientRequestID, "hololive-alarm:")
	assert.Equal(t, testAlarmRoomID, req.ReceiverName)
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
	start := time.Date(2026, time.May, 16, 12, 10, 0, 0, time.UTC)
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.MinutesUntil = 10
	envelope.Notification.Stream.Status = domain.StreamStatusUpcoming
	envelope.Notification.Stream.StartScheduled = &start

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

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

func TestAlarmDispatchRunnerKaringSplitsMixedLiveCatchupAndPrelive(t *testing.T) {
	start := time.Date(2026, time.May, 16, 12, 10, 0, 0, time.UTC)
	live := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	upcoming := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	live.Notification.MinutesUntil = 5
	upcoming.Notification.MinutesUntil = 5
	live.Notification.Stream.ID = "live"
	upcoming.Notification.Stream.ID = "upcoming"
	live.Notification.Stream.StartActual = &start
	upcoming.Notification.Stream.StartScheduled = &start

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{live, upcoming}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.karingRequests, 2)
	assert.Equal(t, "라이브 시작", sender.karingRequests[0].ExtraArgs["alarm_title"])
	assert.Equal(t, "지금 시작", sender.karingRequests[0].ExtraArgs["time_left"])
	assert.Equal(t, "방송 5분 전 알림", sender.karingRequests[1].ExtraArgs["alarm_title"])
	assert.Equal(t, "5분 후 시작", sender.karingRequests[1].ExtraArgs["time_left"])
}

func TestAlarmDispatchRunnerKaringRequestPreservesConfiguredNickname(t *testing.T) {
	englishName := "Yuuki Sakuna"
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.Channel.Name = "사쿠나"
	envelope.Notification.Channel.EnglishName = &englishName
	envelope.Notification.Stream.Channel = envelope.Notification.Channel
	envelope.Notification.Stream.ChannelName = "사쿠나"

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		notifications: []domain.AlarmNotification{envelope.Notification},
	})

	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Len(t, requests[0].Items, 1)
	assert.Equal(t, "사쿠나", requests[0].Items[0].MemberName)
	assert.Equal(t, "사쿠나", requests[0].Items[0].ChannelName)
}

func TestAlarmDispatchRunnerYouTubeOutboxCommunitySendsKaringRequest(t *testing.T) {
	publishedAt := time.Date(2026, time.May, 16, 10, 30, 0, 0, time.UTC)
	thumbnailURL := "https://yt3.ggpht.com/community-image=s800"
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  testAlarmChannelID,
		MemberName: "Community Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "UgkxPost",
			Payload:   `{"post_id":"UgkxPost","content_text":"／\n\n새 커뮤니티 공지입니다\n두번째줄\n＼","images":[{"url":"https://yt3.ggpht.com/community-image=s288","width":288,"height":288},{"url":"` + thumbnailURL + `","width":800,"height":800}],"published_at":"` + publishedAt.Format(time.RFC3339Nano) + `"}`,
		}},
	}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, testAlarmRoomID, sender.karingRoomID)
	require.Len(t, sender.karingRequests, 1)

	req := sender.karingRequests[0]
	assert.Equal(t, testAlarmRoomID, req.ReceiverName)
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
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindMilestone,
		AlarmType:  domain.AlarmTypeLive,
		ChannelID:  testAlarmChannelID,
		MemberName: "Milestone Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "milestone-1",
			Payload:   `{"milestone":"100만"}`,
		}},
	}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, renderer: newCelebrationTestRenderer(t), karingEnabled: true, maxBatch: 10}

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
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindCommunityPost,
		AlarmType:  domain.AlarmTypeCommunity,
		ChannelID:  testAlarmChannelID,
		MemberName: "Community Member",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "UgkxPost",
			Payload:   `{"post_id":"UgkxPost","content_text":"webpicker community smoke 134905\\nthumbnail/text render check"}`,
		}},
	}

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, alarmDispatchGroup{
		roomID:    testAlarmRoomID,
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
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

			envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
			envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
				Kind:       tc.kind,
				AlarmType:  tc.kind.ToAlarmType(),
				ChannelID:  testAlarmChannelID,
				MemberName: "Content Member",
				Items: []domain.YouTubeOutboxItem{{
					OutboxID:  1,
					ContentID: "content-1",
					Payload:   tc.payload,
				}},
			}

			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

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
		envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

		envelope.Notification.Channel.Name = fmt.Sprintf("Member %d", i+1)
		envelope.Notification.Stream.ID = fmt.Sprintf("stream-%d", i+1)
		envelope.Notification.Stream.Title = fmt.Sprintf("Stream %d", i+1)
		envelopes = append(envelopes, envelope)
	}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{envelopes}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

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
				assert.Empty(t, request.ReceiverName)
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
	assert.Empty(t, requests[0].ReceiverName)
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, testAlarmRoomID, sender.roomID)
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

func TestAlarmDispatchRunnerQuarantinesPGSendFailureAfterMarkSending(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

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
	karingErr := fmt.Errorf("iris send karing content list: %w", &iris.HTTPError{StatusCode: 502, URL: testKaringContentListPath})
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: karingErr}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Contains(t, consumer.scheduledSendingRetry[0].Retry.LastError, "returned 502")
	assert.Equal(t, dispatchoutbox.ErrorCodeHTTP5xx, consumer.scheduledSendingRetry[0].Retry.LastErrorCode)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerReturnsErrorWhenPostSendQuarantineFails(t *testing.T) {
	quarantineErr := errors.New("quarantine failed")
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:       [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
		quarantineErr: quarantineErr,
	}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	require.ErrorIs(t, err, quarantineErr)
	assert.Empty(t, consumer.scheduledRetry)
}

func TestAlarmDispatchRunnerConsumesAttemptForRenderFailureBeforeMarkSending(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = nil

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	require.NotNil(t, consumer.scheduledRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.preSendRequeued)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markSending)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, sender.messages)
	assert.Empty(t, sender.karingRequests)
}

func TestAlarmDispatchRunnerDoesNotRetryMarkDispatchedFailureAfterSend(t *testing.T) {
	markErr := &dispatchoutbox.PartialTransitionError{
		Action: "mark dispatch deliveries sent", Updated: 0, Expected: 1,
	}
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:           [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
		markDispatchedErr: markErr,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	require.ErrorIs(t, err, markErr)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.requeued)
	require.Len(t, consumer.markDispatched, 1)
}

func TestAlarmDispatchRunnerRunOnceMovesExhaustedRetryToDLQAndReleasesClaims(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchRetryableMaxAttempts - 1})

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: &iris.HTTPError{StatusCode: 503}}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.scheduledSendingRetry)
	require.Len(t, consumer.movedDLQ, 1)
	require.NotNil(t, consumer.movedDLQ[0].Retry)
	assert.Equal(t, alarmDispatchRetryableMaxAttempts, consumer.movedDLQ[0].Retry.Attempt)
	assert.Equal(t, []string{testAlarmClaimKey}, consumer.releasedClaims)
}

func TestAlarmDispatchRunnerKeepsRetryingRetryableCauseBeyondBaseAttemptCap(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchMaxAttempts - 1})

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: &iris.HTTPError{StatusCode: 503}}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.movedDLQ, "retryable cause must not hit the base attempt cap")
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, alarmDispatchMaxAttempts, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.releasedClaims, "claim keys stay held while the envelope is still retryable")
}

func TestAlarmDispatchRunnerTransportFailureRetriesInsteadOfQuarantine(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testKaringContentListPath, Err: errors.New("connection refused")}
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined, "a brief Iris outage must not terminally quarantine the backlog")
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.scheduledRetry, "post-send failure must route through RouteSendingFailures")
	require.Len(t, consumer.scheduledSendingRetry, 1)
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerDeadlineExceededRetriesInsteadOfQuarantine(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{
		karingErr: fmt.Errorf("send iris karing content list: %w", context.DeadlineExceeded),
	}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerNonRetryableHTTPFailureStillQuarantines(t *testing.T) {
	for _, statusCode := range []int{500, 504, 401, 403} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
			sender := &alarmDispatchRunnerTestSender{karingErr: &iris.HTTPError{StatusCode: statusCode}}
			runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			require.Len(t, consumer.quarantined, 1, "ambiguous-outcome status must stay terminal")
			assert.Empty(t, consumer.scheduledSendingRetry)
			assert.Empty(t, consumer.markDispatched)
		})
	}
}

func TestAlarmDispatchRunnerWaitsOnIdleWaiterForEmptyPGBatch(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: false}
	runner := Runner{consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.False(t, keepGoing)
	assert.Equal(t, 1, waiter.waits)
	assert.Zero(t, waiter.resets)
}

func TestAlarmDispatchRunnerResetsIdleWaiterAfterProcessedBatch(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: true}
	runner := Runner{consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.True(t, keepGoing)
	assert.Zero(t, waiter.waits)
	assert.Equal(t, 1, waiter.resets)
}

func TestAlarmDispatchRunnerYieldsAfterMaxBatchesPerWake(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
	}}
	yieldCount := 0
	runner := Runner{
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
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
		{alarmDispatchRunnerTestEnvelope("room-2", nil)},
	}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: false}
	sender := &alarmDispatchRunnerTestSender{}
	runner := &Runner{
		consumer:   consumer,
		sender:     sender,
		renderer:   newAlarmDispatchTestRenderer(t),
		maxBatch:   10,
		idleWaiter: waiter,
	}

	err := runner.Start(t.Context())

	require.NoError(t, err)
	require.Len(t, consumer.markDispatched, 2)
	assert.Equal(t, []string{testAlarmRoomID, "room-2"}, []string{consumer.markDispatched[0].Notification.RoomID, consumer.markDispatched[1].Notification.RoomID})
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
	runner := Runner{
		consumer: consumer,
		sender:   &alarmDispatchRunnerTestSender{},
		maxBatch: 10,
	}

	keepGoing := runner.runStep(ctx)

	assert.False(t, keepGoing)
	assert.Empty(t, consumer.markDispatched)
}

func TestGroupAlarmDispatchEnvelopesSeparatesScheduledMinuteBuckets(t *testing.T) {
	firstStart := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.Stream.StartScheduled = &firstStart
	second.Notification.Stream.StartScheduled = &secondStart

	groups := groupAlarmDispatchEnvelopes([]domain.AlarmQueueEnvelope{first, second})

	assert.Len(t, groups, 2)
}

func TestGroupAlarmDispatchEnvelopesForKaringCollapsesScheduledMinuteBuckets(t *testing.T) {
	firstStart := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.MinutesUntil = 5
	second.Notification.MinutesUntil = 5
	first.Notification.Stream.StartScheduled = &firstStart
	second.Notification.Stream.StartScheduled = &secondStart

	groups := groupAlarmDispatchEnvelopesForKaring([]domain.AlarmQueueEnvelope{first, second}, true)

	require.Len(t, groups, 1)
	assert.Len(t, groups[0].envelopes, 2)
}

func TestRenderAlarmDispatchNotificationGroupUsesCanonicalTemplate(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

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

	message, err := renderAlarmDispatchGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, "", group)

	require.NoError(t, err)
	assert.Equal(t, "## ⏰ 방송 1분 전\n\n"+
		"⏰ **Member1** 방송 3분 전\n[Title1](https://youtube.com/watch?v=abc)\n\n"+
		"⏰ **Member2** 방송 예정\n[Title2](https://youtube.com/watch?v=def)", message)
}

func TestRenderAlarmDispatchNotificationGroupAllLiveCatchupUsesStartingHeader(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.MinutesUntil = 5
	second.Notification.MinutesUntil = 5
	first.Notification.Channel.Name = "Member1"
	second.Notification.Channel.Name = "Member2"
	first.Notification.Stream.ID = "abc"
	second.Notification.Stream.ID = "def"
	first.Notification.Stream.Title = "Title1"
	second.Notification.Stream.Title = "Title2"
	first.Notification.Stream.StartActual = &start
	second.Notification.Stream.StartActual = &start

	group := alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{first.Notification, second.Notification},
	}

	message, err := renderAlarmDispatchNotificationGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, "", group)

	require.NoError(t, err)
	assert.Equal(t, "## 🔴 방송 시작\n\n"+
		"🔴 **Member1** 방송 시작\n[Title1](https://youtube.com/watch?v=abc)\n\n"+
		"🔴 **Member2** 방송 시작\n[Title2](https://youtube.com/watch?v=def)", message)
}

func TestRenderAlarmDispatchNotificationGroupMixedCatchupKeepsConservativeHeader(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.MinutesUntil = 5
	second.Notification.MinutesUntil = 5
	first.Notification.Channel.Name = "LiveMember"
	second.Notification.Channel.Name = "UpcomingMember"
	first.Notification.Stream.ID = "live"
	second.Notification.Stream.ID = "upcoming"
	first.Notification.Stream.Title = "Live Title"
	second.Notification.Stream.Title = "Upcoming Title"
	first.Notification.Stream.StartActual = &start
	second.Notification.Stream.StartScheduled = &start

	group := alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{first.Notification, second.Notification},
	}

	message, err := renderAlarmDispatchNotificationGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, "", group)

	require.NoError(t, err)
	assert.Equal(t, "## ⏰ 방송 5분 전\n\n"+
		"🔴 **LiveMember** 방송 시작\n[Live Title](https://youtube.com/watch?v=live)\n\n"+
		"⏰ **UpcomingMember** 방송 예정\n[Upcoming Title](https://youtube.com/watch?v=upcoming)", message)
}

func TestRenderAlarmDispatchNotificationLiveCatchupUsesRecoveredUpcomingMessage(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ID = "live-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.StartScheduled = &start
	notification.Stream.StartActual = &start

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"## 🔴 **Member** 방송 시작\n[Live Title](https://youtube.com/watch?v=live-1)",
		got,
	)
}

func TestRenderAlarmDispatchNotificationLiveStatusUsesStartingMessage(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ID = "live-status-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.Status = domain.StreamStatusLive

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"## 🔴 **Member** 방송 시작\n[Live Title](https://youtube.com/watch?v=live-status-1)",
		got,
	)
}

func TestRenderAlarmDispatchNotificationUpcomingKeepsPreliveMessage(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ID = "upcoming-1"
	notification.Stream.Title = "Upcoming Title"
	notification.Stream.Status = domain.StreamStatusUpcoming

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"## ⏰ **Member** 방송 5분 전\n[Upcoming Title](https://youtube.com/watch?v=upcoming-1)",
		got,
	)
}

func TestRenderAlarmDispatchNotificationLinksSingleStreamTitle(t *testing.T) {
	const (
		title = "【ホロライブ ドリームス】水着きちゃ!音ゲー初心者!hololive Dreamsやってみる!【#" + util.KakaoZeroWidthSpace +
			"綺々羅々ヴィヴィ #" + util.KakaoZeroWidthSpace + "hololiveDEV_" + util.KakaoZeroWidthSpace +
			"IS #" + util.KakaoZeroWidthSpace + "FLOWGLOW】"
		streamURL = "https://www.youtube.com/watch?v=DCW0CvsJAnw"
	)

	link := streamURL
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = "비비"
	notification.Stream.ID = "DCW0CvsJAnw"
	notification.Stream.Title = title
	notification.Stream.Link = &link
	notification.Stream.Status = domain.StreamStatusUpcoming

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		fmt.Sprintf("## ⏰ **비비** 방송 5분 전\n[%s](%s)", util.MarkdownNeutralize(title), streamURL),
		got,
	)
}

func TestRenderAlarmDispatchNotificationKeepsIntegratedURLsReadable(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = "비비"
	notification.Stream.ID = "integrated-1"
	notification.Stream.Title = "동시송출 방송"
	notification.Stream.IsIntegrated = true
	notification.Stream.ChzzkLiveURL = "https://chzzk.naver.com/live/integrated-1"

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"## ⏰ **비비** 방송 5분 전\n"+util.KakaoZeroWidthSpace+
			"동시송출 방송\nhttps://youtube.com/watch?v=integrated-1 | https://chzzk.naver.com/live/integrated-1",
		got,
	)
}

func TestRenderAlarmDispatchNotificationLinksDirectPlatformTitles(t *testing.T) {
	for _, tt := range []struct {
		name      string
		configure func(*domain.Stream)
		want      string
	}{
		{
			name: "twitch",
			configure: func(stream *domain.Stream) {
				stream.IsTwitchOnly = true
				stream.TwitchLiveURL = "https://www.twitch.tv/holomember"
			},
			want: "## ⏰ **비비** 방송 5분 전\n[플랫폼 방송](https://www.twitch.tv/holomember)",
		},
		{
			name: "chzzk",
			configure: func(stream *domain.Stream) {
				stream.IsChzzkOnly = true
				stream.ChzzkLiveURL = "https://chzzk.naver.com/live/abcdef"
			},
			want: "## ⏰ **비비** 방송 5분 전\n[플랫폼 방송](https://chzzk.naver.com/live/abcdef)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

			notification.MinutesUntil = 5
			notification.Channel.Name = "비비"
			notification.Stream.Title = "플랫폼 방송"
			tt.configure(notification.Stream)

			got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, nil, &notification)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveAlarmDispatchURLFallsBackToYouTubeWhenPlatformURLMissing(t *testing.T) {
	twitchOnlyWithoutURL := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	twitchOnlyWithoutURL.Stream.IsTwitchOnly = true

	chzzkOnlyWithoutURL := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

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

type alarmDispatchRunnerContextConsumer struct {
	alarmDispatchRunnerTestConsumer

	markSendingCtxErr    error
	markDispatchedCtxErr error
	routeSendingCtxErr   error
	quarantineCtxErr     error
	routeSendingDeadline bool
	quarantineDeadline   bool
}

func (c *alarmDispatchRunnerContextConsumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSendingCtxErr = ctx.Err()
	if c.markSendingCtxErr != nil {
		return fmt.Errorf("mark alarm dispatch sending: %w", c.markSendingCtxErr)
	}

	if err := c.alarmDispatchRunnerTestConsumer.MarkSending(ctx, envelopes); err != nil {
		return fmt.Errorf("mark sending: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatchedCtxErr = ctx.Err()
	if c.markDispatchedCtxErr != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", c.markDispatchedCtxErr)
	}

	if err := c.alarmDispatchRunnerTestConsumer.MarkDispatched(ctx, envelopes); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.routeSendingCtxErr = ctx.Err()
	if c.routeSendingCtxErr != nil {
		return fmt.Errorf("route alarm dispatch sending failure: %w", c.routeSendingCtxErr)
	}

	_, c.routeSendingDeadline = ctx.Deadline()

	if err := c.alarmDispatchRunnerTestConsumer.RouteSendingFailures(ctx, retryEnvelopes, dlqEnvelopes); err != nil {
		return fmt.Errorf("route sending failures: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	c.quarantineCtxErr = ctx.Err()
	if c.quarantineCtxErr != nil {
		return fmt.Errorf("quarantine alarm dispatch: %w", c.quarantineCtxErr)
	}

	_, c.quarantineDeadline = ctx.Deadline()

	if err := c.alarmDispatchRunnerTestConsumer.Quarantine(ctx, envelopes, cause); err != nil {
		return fmt.Errorf("quarantine: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("release alarm dispatch claim keys: %w", err)
	}

	if err := c.alarmDispatchRunnerTestConsumer.ReleaseClaimKeys(ctx, claimKeys); err != nil {
		return fmt.Errorf("release claim keys: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) RequeuePreSend(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("requeue alarm dispatch before send: %w", err)
	}

	if err := c.alarmDispatchRunnerTestConsumer.RequeuePreSend(ctx, envelopes); err != nil {
		return fmt.Errorf("requeue pre send: %w", err)
	}

	return nil
}

type alarmDispatchRunnerBlockingSender struct {
	rooms   []string
	onSend  func()
	succeed bool
}

func (s *alarmDispatchRunnerBlockingSender) waitForAttemptEnd(ctx context.Context, roomID string) error {
	s.rooms = append(s.rooms, roomID)
	if s.onSend != nil {
		s.onSend()
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		return errors.New("send alarm dispatch message: attempt context never ended")
	}

	if s.succeed {
		return nil
	}

	return fmt.Errorf("send alarm dispatch message: %w", ctx.Err())
}

func (s *alarmDispatchRunnerBlockingSender) SendMessage(ctx context.Context, roomID, _ string) error {
	if err := s.waitForAttemptEnd(ctx, roomID); err != nil {
		return fmt.Errorf("wait for attempt end: %w", err)
	}

	return nil
}

func (s *alarmDispatchRunnerBlockingSender) SendKaringContentList(ctx context.Context, roomID string, _ *iris.KaringContentListRequest) error {
	if err := s.waitForAttemptEnd(ctx, roomID); err != nil {
		return fmt.Errorf("wait for attempt end: %w", err)
	}

	return nil
}

func TestAlarmDispatchRunnerRoutesSendingRetryWithLiveContextAfterAttemptDeadline(t *testing.T) {
	consumer := &alarmDispatchRunnerContextConsumer{}

	consumer.batches = [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}

	sender := &alarmDispatchRunnerBlockingSender{}
	runner := Runner{
		consumer:       consumer,
		sender:         sender,
		karingEnabled:  true,
		maxBatch:       10,
		attemptTimeout: 50 * time.Millisecond,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.NoError(t, consumer.routeSendingCtxErr, "attempt 만료 뒤에도 실패 라우팅은 살아있는 컨텍스트로 실행돼야 한다")
	assert.True(t, consumer.routeSendingDeadline, "정리 컨텍스트도 시간 상한을 가져야 한다")
	require.Len(t, consumer.scheduledSendingRetry, 1)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerMarksDispatchedAfterAttemptDeadlineExpires(t *testing.T) {
	consumer := &alarmDispatchRunnerContextConsumer{}

	consumer.batches = [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}

	sender := &alarmDispatchRunnerBlockingSender{succeed: true}
	runner := Runner{
		consumer:       consumer,
		sender:         sender,
		karingEnabled:  true,
		maxBatch:       10,
		attemptTimeout: 50 * time.Millisecond,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.NoError(t, consumer.markDispatchedCtxErr, "발송에 성공한 배치는 attempt 만료 뒤에도 sent로 기록돼야 한다")
	require.Len(t, consumer.markDispatched, 1)
	assert.Empty(t, consumer.scheduledSendingRetry)
	assert.Empty(t, consumer.quarantined)
}

func TestAlarmDispatchRunnerStopsRemainingGroupsWhenAttemptDeadlineExpires(t *testing.T) {
	consumer := &alarmDispatchRunnerContextConsumer{}

	consumer.batches = [][]domain.AlarmQueueEnvelope{{
		alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil),
		alarmDispatchRunnerTestEnvelope("room-2", nil),
	}}

	sender := &alarmDispatchRunnerBlockingSender{}
	runner := Runner{
		consumer:       consumer,
		sender:         sender,
		karingEnabled:  true,
		maxBatch:       10,
		attemptTimeout: 50 * time.Millisecond,
	}

	processed, err := runner.runOnce(t.Context())

	assert.True(t, processed)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, []string{testAlarmRoomID}, sender.rooms, "만료된 attempt로 남은 그룹을 더 보내면 미발송 행이 sending으로 굳는다")
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	assert.Empty(t, consumer.quarantined)
}

func TestAlarmDispatchRunnerBoundsStateContextWhenParentCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	consumer := &alarmDispatchRunnerContextConsumer{}

	consumer.batches = [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}

	sender := &alarmDispatchRunnerBlockingSender{onSend: cancel}
	runner := Runner{
		consumer:      consumer,
		sender:        sender,
		karingEnabled: true,
		maxBatch:      10,
	}

	processed, err := runner.runOnce(ctx)

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.quarantined, 1)
	require.NoError(t, consumer.quarantineCtxErr, "프로세스 종료로 부모가 취소돼도 상태 기록은 완료돼야 한다")
	assert.True(t, consumer.quarantineDeadline, "취소를 끊은 정리 컨텍스트에도 시간 상한이 있어야 한다")
}
