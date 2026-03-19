package settlement

import (
	"testing"
	"time"
)

func TestResolveCycleForMoment(t *testing.T) {
	cfg := RoomConfig{BillingAnchorDay: 18, BillingTZ: "Asia/Seoul"}
	kst, _ := time.LoadLocation("Asia/Seoul")

	tests := []struct {
		name      string
		now       time.Time
		wantKey   string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "3/18 당일 → 3/18 회차",
			now:       time.Date(2026, 3, 18, 0, 0, 0, 0, kst),
			wantKey:   "2026-03-18",
			wantStart: "2026-03-18",
			wantEnd:   "2026-04-18",
		},
		{
			name:      "4/1 → 여전히 3/18 회차",
			now:       time.Date(2026, 4, 1, 12, 0, 0, 0, kst),
			wantKey:   "2026-03-18",
			wantStart: "2026-03-18",
			wantEnd:   "2026-04-18",
		},
		{
			name:      "4/17 → 여전히 3/18 회차",
			now:       time.Date(2026, 4, 17, 23, 59, 59, 0, kst),
			wantKey:   "2026-03-18",
			wantStart: "2026-03-18",
			wantEnd:   "2026-04-18",
		},
		{
			name:      "4/18 → 4/18 회차",
			now:       time.Date(2026, 4, 18, 0, 0, 0, 0, kst),
			wantKey:   "2026-04-18",
			wantStart: "2026-04-18",
			wantEnd:   "2026-05-18",
		},
		{
			name:      "3/17 → 2/18 회차",
			now:       time.Date(2026, 3, 17, 23, 59, 59, 0, kst),
			wantKey:   "2026-02-18",
			wantStart: "2026-02-18",
			wantEnd:   "2026-03-18",
		},
		{
			name:      "1/1 → 12/18 회차 (연말 경계)",
			now:       time.Date(2026, 1, 1, 0, 0, 0, 0, kst),
			wantKey:   "2025-12-18",
			wantStart: "2025-12-18",
			wantEnd:   "2026-01-18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			win, err := ResolveCycleForMoment(cfg, tt.now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if win.CycleKey != tt.wantKey {
				t.Errorf("CycleKey = %s, want %s", win.CycleKey, tt.wantKey)
			}
			startLocal := win.StartAt.In(kst).Format("2006-01-02")
			if startLocal != tt.wantStart {
				t.Errorf("StartAt(KST) = %s, want %s", startLocal, tt.wantStart)
			}
			endLocal := win.EndAt.In(kst).Format("2006-01-02")
			if endLocal != tt.wantEnd {
				t.Errorf("EndAt(KST) = %s, want %s", endLocal, tt.wantEnd)
			}
		})
	}
}

func TestNormalizeExplicitCycleKey(t *testing.T) {
	cfg := RoomConfig{BillingAnchorDay: 18, BillingTZ: "Asia/Seoul"}
	kst, _ := time.LoadLocation("Asia/Seoul")
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, kst)

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{"full date", "2026-03-18", "2026-03-18", nil},
		{"short M/D", "3/18", "2026-03-18", nil},
		{"wrong day", "3/15", "", ErrInvalidExplicitCycle},
		{"wrong format", "abc", "", ErrInvalidExplicitCycle},
		{"empty", "", "", nil},
		{"year rollover 12/18 when now is 2026-04", "12/18", "2025-12-18", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeExplicitCycleKey(cfg, tt.raw, now)
			if tt.wantErr != nil {
				if err == nil || err != tt.wantErr {
					t.Errorf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNextCycleStart(t *testing.T) {
	cfg := RoomConfig{BillingAnchorDay: 18, BillingTZ: "Asia/Seoul"}
	kst, _ := time.LoadLocation("Asia/Seoul")

	start := time.Date(2026, 3, 18, 0, 0, 0, 0, kst).UTC()
	next, err := NextCycleStart(cfg, start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantNext := time.Date(2026, 4, 18, 0, 0, 0, 0, kst).UTC()
	if !next.Equal(wantNext) {
		t.Errorf("next = %v, want %v", next, wantNext)
	}
}
