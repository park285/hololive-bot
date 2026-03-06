package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type stubStatsDashboardRepository struct {
	getAllMilestones       func(context.Context, stats.MilestoneFilter) (*stats.MilestoneResult, error)
	getNearMilestoneMember func(context.Context, float64, []uint64, int) ([]stats.NearMilestoneEntry, error)
	getMilestoneStats      func(context.Context) (*stats.MilestoneStats, error)
	countNearMembers       func(context.Context, float64, []uint64) (int, error)
}

func (s *stubStatsDashboardRepository) GetLatestStatsForChannels(context.Context, []string) (map[string]*domain.TimestampedStats, error) {
	return nil, nil
}

func (s *stubStatsDashboardRepository) GetAllMilestones(ctx context.Context, filter stats.MilestoneFilter) (*stats.MilestoneResult, error) {
	if s.getAllMilestones == nil {
		return &stats.MilestoneResult{}, nil
	}
	return s.getAllMilestones(ctx, filter)
}

func (s *stubStatsDashboardRepository) GetNearMilestoneMembers(
	ctx context.Context,
	thresholdPct float64,
	milestones []uint64,
	limit int,
) ([]stats.NearMilestoneEntry, error) {
	if s.getNearMilestoneMember == nil {
		return nil, nil
	}
	return s.getNearMilestoneMember(ctx, thresholdPct, milestones, limit)
}

func (s *stubStatsDashboardRepository) GetMilestoneStats(ctx context.Context) (*stats.MilestoneStats, error) {
	if s.getMilestoneStats == nil {
		return &stats.MilestoneStats{}, nil
	}
	return s.getMilestoneStats(ctx)
}

func (s *stubStatsDashboardRepository) CountNearMilestoneMembers(
	ctx context.Context,
	thresholdPct float64,
	milestones []uint64,
) (int, error) {
	if s.countNearMembers == nil {
		return 0, nil
	}
	return s.countNearMembers(ctx, thresholdPct, milestones)
}

func TestParseIntAndParseFloat(t *testing.T) {
	t.Run("parse int success", func(t *testing.T) {
		got, err := parseInt(" 42 ")
		if err != nil || got != 42 {
			t.Fatalf("parseInt result=%d err=%v", got, err)
		}
	})

	t.Run("parse int invalid", func(t *testing.T) {
		if _, err := parseInt("abc"); err == nil {
			t.Fatal("expected parseInt error")
		}
	})

	t.Run("parse float success", func(t *testing.T) {
		got, err := parseFloat(" 0.95 ")
		if err != nil || got != 0.95 {
			t.Fatalf("parseFloat result=%f err=%v", got, err)
		}
	})

	t.Run("parse float invalid", func(t *testing.T) {
		if _, err := parseFloat("x.y"); err == nil {
			t.Fatal("expected parseFloat error")
		}
	})
}

func TestMilestoneAPIHandler_GetMilestones(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("repo not initialized", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones", nil)
		h.GetMilestones(ctx)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{},
			logger:    newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones?limit=999", nil)
		h.GetMilestones(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("invalid offset", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{},
			logger:    newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones?offset=-1", nil)
		h.GetMilestones(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("repo error", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getAllMilestones: func(context.Context, stats.MilestoneFilter) (*stats.MilestoneResult, error) {
					return nil, errors.New("query failed")
				},
			},
			logger: newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones", nil)
		h.GetMilestones(ctx)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getAllMilestones: func(context.Context, stats.MilestoneFilter) (*stats.MilestoneResult, error) {
					return &stats.MilestoneResult{
						Milestones: []stats.MilestoneEntry{
							{
								ChannelID:  "UC1",
								MemberName: "Sora",
								Type:       "subscribers",
								Value:      1000000,
								AchievedAt: time.Now(),
							},
						},
						Total:  1,
						Limit:  50,
						Offset: 0,
					}, nil
				},
			},
			logger: newDiscardLogger(),
		}}

		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones?channelId=UC1", nil)
		h.GetMilestones(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})
}

func TestMilestoneAPIHandler_GetNearMilestoneMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("repo not initialized", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/near", nil)
		h.GetNearMilestoneMembers(ctx)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("invalid threshold", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{},
			logger:    newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/near?threshold=1.1", nil)
		h.GetNearMilestoneMembers(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("repo error", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getNearMilestoneMember: func(context.Context, float64, []uint64, int) ([]stats.NearMilestoneEntry, error) {
					return nil, errors.New("query failed")
				},
			},
			logger: newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/near", nil)
		h.GetNearMilestoneMembers(ctx)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("success and trim to limit", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getNearMilestoneMember: func(context.Context, float64, []uint64, int) ([]stats.NearMilestoneEntry, error) {
					out := make([]stats.NearMilestoneEntry, 0, 8)
					for i := 0; i < 8; i++ {
						out = append(out, stats.NearMilestoneEntry{
							ChannelID:     "UC",
							MemberName:    "member",
							CurrentSubs:   900000,
							NextMilestone: 1000000,
							Remaining:     100000,
							ProgressPct:   90,
						})
					}
					return out, nil
				},
			},
			logger: newDiscardLogger(),
		}}

		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/near?threshold=0.9", nil)
		h.GetNearMilestoneMembers(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})
}

func TestMilestoneAPIHandler_GetMilestoneStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("repo not initialized", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/stats", nil)
		h.GetMilestoneStats(ctx)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("milestone stats error", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getMilestoneStats: func(context.Context) (*stats.MilestoneStats, error) {
					return nil, errors.New("stats failed")
				},
			},
			logger: newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/stats", nil)
		h.GetMilestoneStats(ctx)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("near summary error", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getMilestoneStats: func(context.Context) (*stats.MilestoneStats, error) {
					return &stats.MilestoneStats{TotalAchieved: 5, RecentAchievements: 1}, nil
				},
				countNearMembers: func(context.Context, float64, []uint64) (int, error) {
					return 0, errors.New("count failed")
				},
			},
			logger: newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/stats", nil)
		h.GetMilestoneStats(ctx)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := &MilestoneAPIHandler{APIHandler: &APIHandler{
			statsRepo: &stubStatsDashboardRepository{
				getMilestoneStats: func(context.Context) (*stats.MilestoneStats, error) {
					return &stats.MilestoneStats{
						TotalAchieved:      10,
						RecentAchievements: 2,
						NotNotifiedCount:   1,
					}, nil
				},
				countNearMembers: func(context.Context, float64, []uint64) (int, error) {
					return 3, nil
				},
			},
			logger: newDiscardLogger(),
		}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/milestones/stats", nil)
		h.GetMilestoneStats(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})
}
