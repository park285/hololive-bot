// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

// MinutesUntilFloor: 기준 시간(reference)으로부터 목표 시간(target)까지 남은 분(minute)을 내림하여 계산합니다.
func MinutesUntilFloor(target *time.Time, reference time.Time) int {
	if target == nil {
		return -1
	}

	if target.Before(reference) {
		return -1
	}

	duration := target.Sub(reference)
	minutesUntil := math.Floor(duration.Minutes())
	if minutesUntil < 0 {
		return -1
	}

	return int(minutesUntil)
}
