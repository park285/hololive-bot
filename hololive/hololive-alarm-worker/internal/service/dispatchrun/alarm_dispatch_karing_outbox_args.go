package dispatchrun

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
)

func buildAlarmDispatchOutboxKaringExtraArgs(ctx context.Context, store *messagestrings.Store, envelope *domain.AlarmQueueEnvelope, itemCount int) iris.KaringTemplateArgs {
	return buildAlarmDispatchOutboxKaringExtraArgsAt(ctx, store, envelope, itemCount, time.Now())
}

func buildAlarmDispatchOutboxKaringExtraArgsAt(ctx context.Context, store *messagestrings.Store, envelope *domain.AlarmQueueEnvelope, itemCount int, now time.Time) iris.KaringTemplateArgs {
	if envelope == nil || envelope.YouTubeOutbox == nil {
		return nil
	}

	if data, ok := alarmDispatchOutboxPremiereData(envelope.YouTubeOutbox); ok {
		if minutesUntil := util.MinutesUntilCeilPtr(data.ScheduledStartAt, now); minutesUntil > 0 {
			return iris.KaringTemplateArgs{
				"alarm_title": fmt.Sprintf(store.GetOrContext(ctx, messagestrings.NamespaceKaring, "outbox_title_video_premiere", "%d분 후 공개 예정"), minutesUntil),
				"time_left":   fmt.Sprintf(store.GetOrContext(ctx, messagestrings.NamespaceKaring, "outbox_time_video_premiere", "%d분 후 공개"), minutesUntil),
			}
		}

		premiereLabel := store.GetOrContext(ctx, messagestrings.NamespaceKaring, "status_video_premiere", "최초공개")

		return iris.KaringTemplateArgs{
			"alarm_title": premiereLabel,
			"time_left":   premiereLabel,
		}
	}

	baseTitle, timeLeft := alarmDispatchOutboxKaringLabels(ctx, store, envelope.YouTubeOutbox.Kind)

	return iris.KaringTemplateArgs{
		"alarm_title": alarmDispatchKaringTitleWithCount(ctx, store, baseTitle, itemCount),
		"time_left":   timeLeft,
	}
}

func alarmDispatchOutboxPremiereData(payload *domain.YouTubeOutboxDispatchPayload) (alarmDispatchKaringVideoPayload, bool) {
	if payload == nil || payload.Kind != domain.OutboxKindNewVideo || len(payload.Items) != 1 {
		return alarmDispatchKaringVideoPayload{}, false
	}

	var data alarmDispatchKaringVideoPayload

	if err := jsonv2.Unmarshal([]byte(payload.Items[0].Payload), &data); err != nil {
		return alarmDispatchKaringVideoPayload{}, false
	}

	if data.IsPremiere == nil || !*data.IsPremiere {
		return alarmDispatchKaringVideoPayload{}, false
	}

	return data, true
}

func alarmDispatchOutboxPremiereMinutes(payload *domain.YouTubeOutboxDispatchPayload, now time.Time) (int, bool) {
	data, ok := alarmDispatchOutboxPremiereData(payload)
	if !ok {
		return 0, false
	}

	minutesUntil := util.MinutesUntilCeilPtr(data.ScheduledStartAt, now)
	if minutesUntil <= 0 {
		return 0, false
	}

	return minutesUntil, true
}

type alarmDispatchKaringLabel struct {
	alarmTitleKey      string
	alarmTitleFallback string
	timeLeftKey        string
	timeLeftFallback   string
}

var alarmDispatchOutboxKaringLabelsByKind = map[domain.OutboxKind]alarmDispatchKaringLabel{
	domain.OutboxKindCommunityPost: {alarmTitleKey: "outbox_title_community", alarmTitleFallback: "커뮤니티 알림", timeLeftKey: "outbox_time_community", timeLeftFallback: "새 커뮤니티"},
	domain.OutboxKindNewShort:      {alarmTitleKey: "outbox_title_shorts", alarmTitleFallback: "쇼츠 알림", timeLeftKey: "outbox_time_shorts", timeLeftFallback: "새 쇼츠"},
	domain.OutboxKindNewVideo:      {alarmTitleKey: "outbox_title_video", alarmTitleFallback: "새 영상", timeLeftKey: "outbox_time_video", timeLeftFallback: "새 영상"},
	domain.OutboxKindLiveStream:    {alarmTitleKey: "outbox_title_live", alarmTitleFallback: "방송 알림", timeLeftKey: "outbox_time_live", timeLeftFallback: "방송 알림"},
}

func alarmDispatchOutboxKaringLabels(ctx context.Context, store *messagestrings.Store, kind domain.OutboxKind) (alarmTitle, timeLeft string) {
	label, ok := alarmDispatchOutboxKaringLabelsByKind[kind]
	if !ok {
		label = alarmDispatchKaringLabel{alarmTitleKey: "title_fallback", alarmTitleFallback: "알림", timeLeftKey: "time_fallback", timeLeftFallback: "새 알림"}
	}

	return store.GetOrContext(ctx, messagestrings.NamespaceKaring, label.alarmTitleKey, label.alarmTitleFallback),
		store.GetOrContext(ctx, messagestrings.NamespaceKaring, label.timeLeftKey, label.timeLeftFallback)
}

func alarmDispatchKaringTitleWithCount(ctx context.Context, store *messagestrings.Store, title string, itemCount int) string {
	if itemCount <= 1 {
		return title
	}

	return fmt.Sprintf(store.GetOrContext(ctx, messagestrings.NamespaceKaring, "count_suffix", "%s · %d건"), title, itemCount)
}
