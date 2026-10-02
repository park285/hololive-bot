package format

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildTemplateDataClassifiesPremiere(t *testing.T) {
	t.Parallel()

	scheduled := time.Now().UTC().Add(30 * time.Minute)
	item := &domain.YouTubeNotificationOutbox{
		Kind:    domain.OutboxKindNewVideo,
		Payload: `{"video_id":"premiere","title":"최초공개","scheduled_start_at":"` + scheduled.Format(time.RFC3339Nano) + `","is_premiere":true}`,
	}

	data, err := (&MessageFormatter{}).BuildTemplateData("아크로라", item)
	if err != nil {
		t.Fatalf("BuildTemplateData() error = %v", err)
	}

	if !data.IsPremiere || !data.IsUpcomingPremiere {
		t.Fatalf("premiere flags = (%t, %t), want (true, true)", data.IsPremiere, data.IsUpcomingPremiere)
	}

	if data.MinutesUntilPremiere != 30 {
		t.Fatalf("minutes until premiere = %d, want 30", data.MinutesUntilPremiere)
	}
}

func TestBuildTemplateDataKeepsRegularVideoClassification(t *testing.T) {
	t.Parallel()

	item := &domain.YouTubeNotificationOutbox{
		Kind:    domain.OutboxKindNewVideo,
		Payload: `{"video_id":"upload","title":"일반 업로드"}`,
	}

	data, err := (&MessageFormatter{}).BuildTemplateData("아크로라", item)
	if err != nil {
		t.Fatalf("BuildTemplateData() error = %v", err)
	}

	if data.IsPremiere || data.IsUpcomingPremiere || data.MinutesUntilPremiere != 0 {
		t.Fatalf("regular video was classified as premiere: %+v", data)
	}
}

func TestBuildTemplateDataKeepsPremiereClassificationWithoutFutureSchedule(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"past schedule":    `{"video_id":"premiere","is_premiere":true,"scheduled_start_at":"2000-01-01T00:00:00Z"}`,
		"missing schedule": `{"video_id":"premiere","is_premiere":true}`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, err := (&MessageFormatter{}).BuildTemplateData("아크로라", &domain.YouTubeNotificationOutbox{
				Kind:    domain.OutboxKindNewVideo,
				Payload: payload,
			})
			if err != nil {
				t.Fatalf("BuildTemplateData() error = %v", err)
			}

			if !data.IsPremiere || data.IsUpcomingPremiere {
				t.Fatalf("premiere flags = (%t, %t), want (true, false)", data.IsPremiere, data.IsUpcomingPremiere)
			}
		})
	}
}
