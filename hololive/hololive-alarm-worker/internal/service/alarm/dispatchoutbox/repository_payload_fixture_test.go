package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const fixtureCelebrationDate = "2026-05-26"

type ledgerPayloadFixture struct {
	name        string
	envelope    AlarmQueueEnvelope
	wantPayload string
	wantHash    string
}

// event payload 바이트와 hash는 이미 저장된 event 행과의 동일성 기준이다. 바이트가 바뀌면 기존 event에 합류하는
// 방의 delivery가 충돌로 거절되므로 출처별 대표 envelope의 적재 바이트를 고정한다.
func TestBuildLedgerRowsEventPayloadBytesAreStable(t *testing.T) {
	t.Parallel()

	for _, tt := range ledgerPayloadFixtures() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event, _, err := buildLedgerRows(&tt.envelope)
			require.NoError(t, err)
			require.Equal(t, tt.wantPayload, string(event.Payload))
			require.Equal(t, tt.wantHash, event.PayloadHash)
		})
	}
}

func ledgerPayloadFixtures() []ledgerPayloadFixture {
	start := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	viewers := 1234

	return []ledgerPayloadFixture{
		{
			name: "live notification",
			envelope: AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{
					AlarmType:    domain.AlarmTypeLive,
					RoomID:       testRoomID,
					Users:        []string{testUserName},
					Channel:      &domain.Channel{ID: testChannelID},
					Stream:       &domain.Stream{ID: testStreamID, ChannelID: testChannelID, Title: "방송 제목", StartScheduled: &start, ViewerCount: &viewers},
					MinutesUntil: 5,
				},
				ClaimKeys:  []string{"notified:claim:room-1"},
				EnqueuedAt: "2026-05-12T02:55:00Z",
				Version:    1,
			},
			wantPayload: `{"notification":{"alarm_type":"LIVE","channel":{"id":"channel-1","name":""},"stream":{"id":"stream-1","title":"방송 제목","channel_id":"channel-1","channel_name":"","status":"","start_scheduled":"2026-05-12T03:00:00Z","is_premiere":false},"minutes_until":5},"version":1}`,
			wantHash:    "12a30011976716364cfd20b3616d67bd101d7f63cc73e7116f5002151dc65b1a",
		},
		{
			name: "celebration",
			envelope: AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{AlarmType: domain.AlarmTypeBirthday, RoomID: testRoomID, Channel: &domain.Channel{ID: testYouTubeChannelID}},
				SourceKind:   domain.AlarmDispatchSourceKindCelebration,
				Celebration: &domain.CelebrationDispatchPayload{
					Kind: domain.CelebrationKindBirthday, MemberID: 101, MemberName: "Test", ChannelID: testYouTubeChannelID, Date: fixtureCelebrationDate,
				},
				Version: 1,
			},
			wantPayload: `{"notification":{"alarm_type":"BIRTHDAY","channel":{"id":"UC_test","name":""},"stream":null,"minutes_until":0},"source_kind":"celebration","celebration":{"kind":"birthday","member_id":101,"member_name":"Test","channel_id":"UC_test","ordinal":0,"years":0,"date":"2026-05-26"},"version":1}`,
			wantHash:    "35e9009c1c18493006f01c8e6388b6b0a43470131a942cc8f028a69d0fa20cdf",
		},
		{
			name: "delivery digest",
			envelope: AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{AlarmType: domain.AlarmTypeCommunity, RoomID: testRoomID},
				SourceKind:   domain.AlarmDispatchSourceKindDeliveryDigest,
				DeliveryDigest: &domain.DeliveryDigestDispatchPayload{
					Kind: domain.DeliveryKindMajorEventMonthly, PeriodKey: testDigestPeriodKey, PreRenderedMessage: "8월 주요 이벤트",
				},
				Version: 1,
			},
			wantPayload: `{"notification":{"alarm_type":"COMMUNITY","channel":null,"stream":null,"minutes_until":0},"source_kind":"delivery_digest","delivery_digest":{"kind":"MAJOR_EVENT_MONTHLY","period_key":"2026-08","pre_rendered_message":"8월 주요 이벤트"},"version":1}`,
			wantHash:    "1eb51f7d47f6eaed4c4c08af66951a4a4c4e5f288bda98698e5e2bdaee9a6e08",
		},
		{
			name: "x space",
			envelope: AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: testRoomID},
				SourceKind:   domain.AlarmDispatchSourceKindXSpace,
				XSpace:       &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: testYouTubeChannelID, Title: "이야기", StartedAt: start},
				Version:      1,
			},
			wantPayload: `{"notification":{"alarm_type":"LIVE","channel":null,"stream":null,"minutes_until":0},"source_kind":"x_space","x_space":{"space_id":"1abc","creator_id":"123","channel_id":"UC_test","title":"이야기","started_at":"2026-05-12T03:00:00Z"},"version":1}`,
			wantHash:    "beaa31ae904957cdb34415af1716307577ea78de09003cee7e98644ce8964d43",
		},
	}
}
