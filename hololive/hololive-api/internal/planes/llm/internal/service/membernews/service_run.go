package membernews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/filter"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
)

type digestRun struct {
	service    *Service
	period     model.Period
	now        time.Time
	candidates *filter.PreparedCandidates
}

// PrepareDigestRun은 공통 후보 조회 실패를 실행 전체 오류로 반환합니다. 이전 후보나 빈 성공으로 대체하지 않습니다.
// 후보·clock은 run에 고정하고 방 멤버·alias와 guard/요약/render/enqueue 실패 단위는 방별로 유지합니다.
func (s *Service) PrepareDigestRun(ctx context.Context, period model.Period, now time.Time) (model.DigestGenerator, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("membernews repository is nil")
	}

	period = model.NormalizePeriod(period)

	candidates, err := s.prepareCandidates(ctx, period, now)
	if err != nil {
		return nil, fmt.Errorf("prepare member news run: %w", err)
	}

	return &digestRun{service: s, period: period, now: now, candidates: candidates}, nil
}

func (s *Service) prepareCandidates(ctx context.Context, period model.Period, now time.Time) (*filter.PreparedCandidates, error) {
	candidates, err := s.repository.ListActiveMajorEventsForPeriod(ctx, period, now)
	if err != nil {
		return nil, fmt.Errorf("list active major events for period: %w", err)
	}

	return filter.PrepareCandidates(candidates, period, now), nil
}

func (r *digestRun) GenerateRoomDigest(ctx context.Context, roomID string, period model.Period) (*model.Digest, error) {
	if err := r.service.validateGenerateRoomDigestRequest(roomID); err != nil {
		return nil, fmt.Errorf("validate generate room digest request: %w", err)
	}

	if model.NormalizePeriod(period) != r.period {
		return nil, errors.New("membernews digest period differs from prepared run")
	}

	members, err := r.service.repository.GetRoomMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room members: %w", err)
	}

	if len(members) == 0 {
		return nil, model.ErrNoSubscribedMembers
	}

	return r.service.generatePreparedRoomDigest(ctx, roomID, r.period, r.now, members, r.candidates)
}
