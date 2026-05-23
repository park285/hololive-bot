package streamschedule

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/hololive-bot/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

type stubStreamProvider struct {
	streams []*domain.Stream
	err     error
}

func (s *stubStreamProvider) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return s.streams, s.err
}

func (s *stubStreamProvider) GetChannel(context.Context, string) (*domain.Channel, error) {
	return nil, nil
}

type stubMemberDataProvider struct {
	member *domain.Member
}

func (s *stubMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member {
	if s.member != nil && s.member.ChannelID == channelID {
		return s.member
	}
	return nil
}

func (s *stubMemberDataProvider) FindMemberByName(string) *domain.Member  { return nil }
func (s *stubMemberDataProvider) FindMemberByAlias(string) *domain.Member { return nil }
func (s *stubMemberDataProvider) GetChannelIDs() []string                 { return nil }
func (s *stubMemberDataProvider) GetAllMembers() []*domain.Member         { return nil }
func (s *stubMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider {
	return s
}
func (s *stubMemberDataProvider) FindMembersByName(string) []*domain.Member  { return nil }
func (s *stubMemberDataProvider) FindMembersByAlias(string) []*domain.Member { return nil }

func TestServiceGetChannelSchedule_AddsChzzkSchedules(t *testing.T) {
	t.Parallel()

	scheduledStartAt := time.Now().In(time.FixedZone("KST", 9*60*60)).Add(2 * time.Hour).Format(chzzk.ChzzkTimeLayout)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/v1/channels/chzzk-1/scheduled-lives" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"content": map[string]any{
				"scheduledLives": []map[string]any{
					{
						"liveTitle":        "치지직 일정",
						"scheduledStartAt": scheduledStartAt,
					},
				},
			},
		})
	}))
	defer server.Close()

	service := NewService(
		&stubStreamProvider{},
		chzzk.NewClient(http.DefaultClient, server.URL, slog.New(slog.DiscardHandler)),
		&stubMemberDataProvider{
			member: &domain.Member{
				ChannelID:      "yt-1",
				Name:           "유니",
				ChzzkChannelID: "chzzk-1",
			},
		},
		slog.New(slog.DiscardHandler),
	)

	streams, err := service.GetChannelSchedule(t.Context(), "yt-1", 24, true)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	if streams[0].GetYouTubeURL() != "https://chzzk.naver.com/live/chzzk-1" {
		t.Fatalf("url = %q, want chzzk live url", streams[0].GetYouTubeURL())
	}

	if streams[0].ChzzkChannelID != "chzzk-1" {
		t.Fatalf("ChzzkChannelID = %q, want chzzk-1", streams[0].ChzzkChannelID)
	}
}

func TestServiceGetChannelSchedule_MergesMatchingHolodexStream(t *testing.T) {
	t.Parallel()

	scheduled := time.Now().In(time.FixedZone("KST", 9*60*60)).Add(2 * time.Hour).Truncate(time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/v1/channels/chzzk-1/scheduled-lives" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"content": map[string]any{
				"scheduledLives": []map[string]any{
					{
						"liveTitle":        "동시송출 일정",
						"scheduledStartAt": scheduled.Format(chzzk.ChzzkTimeLayout),
					},
				},
			},
		})
	}))
	defer server.Close()

	service := NewService(
		&stubStreamProvider{
			streams: []*domain.Stream{
				{
					ID:             "yt-video",
					Title:          "동시송출 일정",
					ChannelID:      "yt-1",
					ChannelName:    "유니",
					Status:         domain.StreamStatusUpcoming,
					StartScheduled: &scheduled,
				},
			},
		},
		chzzk.NewClient(http.DefaultClient, server.URL, slog.New(slog.DiscardHandler)),
		&stubMemberDataProvider{
			member: &domain.Member{
				ChannelID:      "yt-1",
				Name:           "유니",
				ChzzkChannelID: "chzzk-1",
			},
		},
		slog.New(slog.DiscardHandler),
	)

	streams, err := service.GetChannelSchedule(t.Context(), "yt-1", 24, true)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}

	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}

	if !streams[0].IsIntegrated {
		t.Fatal("expected merged stream to be marked integrated")
	}

	if streams[0].ChzzkChannelID != "chzzk-1" {
		t.Fatalf("ChzzkChannelID = %q, want chzzk-1", streams[0].ChzzkChannelID)
	}
}

func TestServiceGetChannelSchedule_FiltersChzzkSchedulesOutsideHoursWindow(t *testing.T) {
	t.Parallel()

	farFuture := time.Now().In(time.FixedZone("KST", 9*60*60)).Add(48 * time.Hour).Format(chzzk.ChzzkTimeLayout)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/v1/channels/chzzk-1/scheduled-lives" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"content": map[string]any{
				"scheduledLives": []map[string]any{
					{
						"liveTitle":        "먼 미래 일정",
						"scheduledStartAt": farFuture,
					},
				},
			},
		})
	}))
	defer server.Close()

	service := NewService(
		&stubStreamProvider{},
		chzzk.NewClient(http.DefaultClient, server.URL, slog.New(slog.DiscardHandler)),
		&stubMemberDataProvider{
			member: &domain.Member{
				ChannelID:      "yt-1",
				Name:           "유니",
				ChzzkChannelID: "chzzk-1",
			},
		},
		slog.New(slog.DiscardHandler),
	)

	streams, err := service.GetChannelSchedule(t.Context(), "yt-1", 24, true)
	if err != nil {
		t.Fatalf("GetChannelSchedule() error = %v", err)
	}

	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
}
