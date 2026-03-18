package settlement

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func loadLocation(tz string) (*time.Location, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("load location: %w", err)
	}
	return loc, nil
}

// ResolveCycleForMoment: 주어진 시각이 속한 앵커 회차를 계산합니다.
func ResolveCycleForMoment(cfg RoomConfig, now time.Time) (CycleWindow, error) {
	loc, err := loadLocation(cfg.BillingTZ)
	if err != nil {
		return CycleWindow{}, err
	}

	localNow := now.In(loc)
	thisMonthAnchor := time.Date(
		localNow.Year(),
		localNow.Month(),
		cfg.BillingAnchorDay,
		0, 0, 0, 0,
		loc,
	)

	var startLocal time.Time
	if !localNow.Before(thisMonthAnchor) {
		startLocal = thisMonthAnchor
	} else {
		prev := localNow.AddDate(0, -1, 0)
		startLocal = time.Date(
			prev.Year(),
			prev.Month(),
			cfg.BillingAnchorDay,
			0, 0, 0, 0,
			loc,
		)
	}

	endLocal := startLocal.AddDate(0, 1, 0)

	return CycleWindow{
		CycleKey: startLocal.Format("2006-01-02"),
		StartAt:  startLocal.UTC(),
		EndAt:    endLocal.UTC(),
	}, nil
}

// NextCycleStart: 이미 알고 있는 회차 시작 시각으로부터 다음 회차 시작 시각을 계산합니다.
func NextCycleStart(cfg RoomConfig, startUTC time.Time) (time.Time, error) {
	loc, err := loadLocation(cfg.BillingTZ)
	if err != nil {
		return time.Time{}, err
	}

	startLocal := startUTC.In(loc)
	nextLocal := startLocal.AddDate(0, 1, 0)

	return time.Date(
		nextLocal.Year(),
		nextLocal.Month(),
		cfg.BillingAnchorDay,
		0, 0, 0, 0,
		loc,
	).UTC(), nil
}

// CycleKeyFromStart: 회차 시작 UTC 시각으로 cycle_key를 계산합니다.
func CycleKeyFromStart(cfg RoomConfig, startUTC time.Time) (string, error) {
	loc, err := loadLocation(cfg.BillingTZ)
	if err != nil {
		return "", err
	}
	return startUTC.In(loc).Format("2006-01-02"), nil
}

// NormalizeExplicitCycleKey: 사용자가 입력한 회차 인자를 YYYY-MM-DD 형식으로 정규화합니다.
// 지원 형식: YYYY-MM-DD, M/D, MM/DD
func NormalizeExplicitCycleKey(cfg RoomConfig, raw string, now time.Time) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	loc, err := loadLocation(cfg.BillingTZ)
	if err != nil {
		return "", err
	}

	if full, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
		if full.Day() != cfg.BillingAnchorDay {
			return "", ErrInvalidExplicitCycle
		}
		return full.Format("2006-01-02"), nil
	}

	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", ErrInvalidExplicitCycle
	}

	month, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || month < 1 || month > 12 {
		return "", ErrInvalidExplicitCycle
	}
	day, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || day != cfg.BillingAnchorDay {
		return "", ErrInvalidExplicitCycle
	}

	currentWin, err := ResolveCycleForMoment(cfg, now)
	if err != nil {
		return "", err
	}
	currentLocalStart := currentWin.StartAt.In(loc)

	candidate := time.Date(currentLocalStart.Year(), time.Month(month), day, 0, 0, 0, 0, loc)
	if candidate.After(currentLocalStart) {
		candidate = candidate.AddDate(-1, 0, 0)
	}

	return candidate.Format("2006-01-02"), nil
}
