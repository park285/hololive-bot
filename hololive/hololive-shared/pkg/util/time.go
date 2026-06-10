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

// KSTZone은 KST(UTC+9) 고정 오프셋 Location입니다.
// time.LoadLocation 기반 kstLocation과 달리 tzdata 없이 동작합니다.
var KSTZone = time.FixedZone("KST", 9*60*60)

var kstLocation *time.Location

func init() {
	var err error
	kstLocation, err = time.LoadLocation("Asia/Seoul")
	if err != nil {
		kstLocation = time.FixedZone("KST", 9*60*60)
	}
}

func FirstNonNilTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func ToKST(t time.Time) time.Time {
	return t.In(kstLocation)
}

func FormatKST(t time.Time, layout string) string {
	return t.In(kstLocation).Format(layout)
}

func NowKST() time.Time {
	return time.Now().In(kstLocation)
}

func MinutesUntilFloorPtr(target *time.Time, reference time.Time) int {
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
