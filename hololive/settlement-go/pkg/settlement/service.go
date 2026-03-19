package settlement

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Service: 정산 도메인 로직.
type Service struct {
	repo *Repository
}

// NewService: Service 생성자.
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetStatus: 현재 시각 기준 회차를 보장하고 해당 회차 현황을 반환합니다.
func (s *Service) GetStatus(ctx context.Context, roomID string, now time.Time) (*Cycle, []PaymentStatus, error) {
	var cycle *Cycle
	var statuses []PaymentStatus

	err := s.repo.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.repo.LockRoomTx(ctx, tx, roomID); err != nil {
			return err
		}

		cfg, err := s.repo.GetRoomConfigTx(ctx, tx, roomID)
		if err != nil {
			return err
		}

		cycle, err = s.ensureCyclesUpToLocked(ctx, tx, cfg, now.UTC())
		if err != nil {
			return err
		}

		statuses, err = s.repo.GetPaymentStatusesTx(ctx, tx, cycle.ID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return cycle, statuses, nil
}

// MarkPaid: !정산완료를 처리합니다.
func (s *Service) MarkPaid(ctx context.Context, in MarkPaidInput) (*Cycle, []PaymentStatus, error) {
	var cycle *Cycle
	var statuses []PaymentStatus

	err := s.repo.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.repo.LockRoomTx(ctx, tx, in.RoomID); err != nil {
			return err
		}

		cfg, err := s.repo.GetRoomConfigTx(ctx, tx, in.RoomID)
		if err != nil {
			return err
		}

		// webhook 재시도 등 같은 외부 이벤트가 중복으로 들어오면 이미 처리한 회차를 그대로 반환합니다.
		ref, err := s.repo.FindPaymentEventBySourceTx(ctx, tx, in.SourceType, in.SourceEventID)
		if err != nil {
			return err
		}
		if ref != nil {
			cycle, err = s.repo.GetCycleByIDTx(ctx, tx, ref.CycleID)
			if err != nil {
				return err
			}
			statuses, err = s.repo.GetPaymentStatusesTx(ctx, tx, ref.CycleID)
			return err
		}

		currentCycle, err := s.ensureCyclesUpToLocked(ctx, tx, cfg, in.PaidAt.UTC())
		if err != nil {
			return err
		}

		member, err := s.repo.FindMemberByUserIDTx(ctx, tx, in.RoomID, in.KakaoUserID)
		if err != nil {
			return err
		}
		if member == nil {
			return ErrNotRegisteredMember
		}

		target, err := s.resolvePayTargetLocked(ctx, tx, cfg, member.ID, currentCycle, in.ExplicitCycleKey, in.PaidAt.UTC())
		if err != nil {
			return err
		}

		if target.PaidAt == nil {
			if err := s.repo.MarkPaidTx(ctx, tx, target.CycleID, member.ID, in.PaidAt.UTC()); err != nil {
				return err
			}
			if err := s.repo.InsertPaymentEventTx(ctx, tx, target.CycleID, member.ID, in.SourceType, in.SourceEventID, in.PaidAt.UTC()); err != nil {
				return err
			}
		}

		cycle, err = s.repo.GetCycleByIDTx(ctx, tx, target.CycleID)
		if err != nil {
			return err
		}
		statuses, err = s.repo.GetPaymentStatusesTx(ctx, tx, target.CycleID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return cycle, statuses, nil
}

// GetReminderPayload: 알람이 필요한 시점이면 현재 회차와 미납자 목록을 반환합니다.
func (s *Service) GetReminderPayload(ctx context.Context, roomID string, now time.Time) (*Cycle, []string, error) {
	var cycle *Cycle
	var unpaid []string

	err := s.repo.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.repo.LockRoomTx(ctx, tx, roomID); err != nil {
			return err
		}

		cfg, err := s.repo.GetRoomConfigTx(ctx, tx, roomID)
		if err != nil {
			return err
		}

		shouldSend, err := s.shouldSendReminder(cfg, now.UTC())
		if err != nil {
			return err
		}
		if !shouldSend {
			return nil
		}

		cycle, err = s.ensureCyclesUpToLocked(ctx, tx, cfg, now.UTC())
		if err != nil {
			return err
		}

		unpaid, err = s.repo.GetUnpaidMemberNamesTx(ctx, tx, cycle.ID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return cycle, unpaid, nil
}

func (s *Service) shouldSendReminder(cfg RoomConfig, now time.Time) (bool, error) {
	loc, err := loadLocation(cfg.BillingTZ)
	if err != nil {
		return false, err
	}
	localNow := now.In(loc)
	startDay := cfg.BillingAnchorDay - 1
	if startDay < 1 {
		startDay = 1
	}
	return localNow.Day() >= startDay, nil
}

func (s *Service) ensureCyclesUpToLocked(ctx context.Context, tx pgx.Tx, cfg RoomConfig, now time.Time) (*Cycle, error) {
	targetWin, err := ResolveCycleForMoment(cfg, now)
	if err != nil {
		return nil, err
	}

	targetCycle, err := s.repo.FindCycleByKeyTx(ctx, tx, cfg.RoomID, targetWin.CycleKey)
	if err != nil {
		return nil, err
	}
	if targetCycle != nil {
		return targetCycle, nil
	}

	latest, err := s.repo.GetLatestCycleTx(ctx, tx, cfg.RoomID)
	if err != nil {
		return nil, err
	}

	if latest == nil {
		return s.createCycleSnapshotLocked(ctx, tx, cfg, targetWin)
	}

	if latest.PeriodStartAt.After(targetWin.StartAt) {
		return nil, fmt.Errorf("latest cycle is newer than target cycle (room=%s latest=%s target=%s)", cfg.RoomID, latest.CycleKey, targetWin.CycleKey)
	}

	nextStart, err := NextCycleStart(cfg, latest.PeriodStartAt)
	if err != nil {
		return nil, err
	}

	for !nextStart.After(targetWin.StartAt) {
		nextEnd, err := NextCycleStart(cfg, nextStart)
		if err != nil {
			return nil, err
		}
		cycleKey, err := CycleKeyFromStart(cfg, nextStart)
		if err != nil {
			return nil, err
		}

		existing, err := s.repo.FindCycleByKeyTx(ctx, tx, cfg.RoomID, cycleKey)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			if _, err := s.createCycleSnapshotLocked(ctx, tx, cfg, CycleWindow{
				CycleKey: cycleKey,
				StartAt:  nextStart,
				EndAt:    nextEnd,
			}); err != nil {
				return nil, err
			}
		}

		nextStart = nextEnd
	}

	return s.repo.FindCycleByKeyTx(ctx, tx, cfg.RoomID, targetWin.CycleKey)
}

func (s *Service) createCycleSnapshotLocked(ctx context.Context, tx pgx.Tx, cfg RoomConfig, win CycleWindow) (*Cycle, error) {
	members, err := s.repo.ListActiveMembersAtTx(ctx, tx, cfg.RoomID, win.StartAt)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, ErrNoActiveMembers
	}

	cycle, err := s.repo.InsertCycleTx(ctx, tx, Cycle{
		RoomID:              cfg.RoomID,
		CycleKey:            win.CycleKey,
		PeriodStartAt:       win.StartAt,
		PeriodEndAt:         win.EndAt,
		TotalAmount:         cfg.TotalAmount,
		PerPerson:           cfg.PerPerson,
		BillingAnchorDay:    cfg.BillingAnchorDay,
		MemberCountSnapshot: len(members),
	})
	if err != nil {
		return nil, err
	}

	for _, m := range members {
		if err := s.repo.InsertCyclePaymentRowTx(ctx, tx, cycle.ID, m.MemberID, m.MemberName); err != nil {
			return nil, err
		}
	}

	return cycle, nil
}

func (s *Service) resolvePayTargetLocked(
	ctx context.Context,
	tx pgx.Tx,
	cfg RoomConfig,
	memberID int,
	currentCycle *Cycle,
	explicitRaw string,
	now time.Time,
) (*PaymentTarget, error) {
	explicitRaw = strings.TrimSpace(explicitRaw)
	if explicitRaw != "" {
		normalized, err := NormalizeExplicitCycleKey(cfg, explicitRaw, now)
		if err != nil {
			return nil, err
		}
		if normalized > currentCycle.CycleKey {
			return nil, ErrFutureCycleNotAllowed
		}

		target, err := s.repo.FindPaymentTargetByCycleKeyTx(ctx, tx, cfg.RoomID, memberID, normalized)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return nil, ErrCycleNotFoundForMember
		}
		return target, nil
	}

	pending, err := s.repo.ListPendingPaymentTargetsUpToTx(ctx, tx, cfg.RoomID, memberID, currentCycle.PeriodStartAt)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return nil, ErrNoPendingCycle
	}
	if len(pending) > 1 && cfg.RequireExplicitForMultiple {
		keys := make([]string, 0, len(pending))
		for _, p := range pending {
			keys = append(keys, p.CycleKey)
		}
		return nil, &MultiplePendingCyclesError{CycleKeys: keys}
	}

	return &pending[0], nil
}
