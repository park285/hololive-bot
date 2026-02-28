package util

import (
	"math"
	"time"
)

var kstLocation *time.Location

func init() {
	var err error
	kstLocation, err = time.LoadLocation("Asia/Seoul")
	if err != nil {
		kstLocation = time.FixedZone("KST", 9*60*60)
	}
}

// ToKST: 주어진 시간을 한국 표준시(KST)로 변환합니다.
func ToKST(t time.Time) time.Time {
	return t.In(kstLocation)
}

// FormatKST: 주어진 시간을 KST 기준으로 지정된 포맷 문자열로 변환합니다.
func FormatKST(t time.Time, layout string) string {
	return t.In(kstLocation).Format(layout)
}

// NowKST: 현재 시간을 KST 기준으로 반환합니다.
func NowKST() time.Time {
	return time.Now().In(kstLocation)
}

// MinutesUntilCeil: 기준 시간(reference)으로부터 목표 시간(target)까지 남은 분(minute)을 올림하여 계산합니다.
func MinutesUntilCeil(target *time.Time, reference time.Time) int {
	if target == nil {
		return -1
	}

	if target.Before(reference) {
		return -1
	}

	duration := target.Sub(reference)
	minutesUntil := math.Ceil(duration.Minutes())
	if minutesUntil < 0 {
		return -1
	}

	return int(minutesUntil)
}
