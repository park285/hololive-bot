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

package delivery

import "time"

type SendResult struct {
	Attempted   int      // claim 획득하여 발송 시도한 room 수
	Sent        int      // 발송 성공 (enqueue 성공)
	Skipped     int      // 이미 claim됨 (기 발송) → skip
	Failed      int      // 발송 실패
	FailedRooms []string // 실패 room ID 목록
}

func (r *SendResult) Merge(child SendResult) {
	r.Attempted += child.Attempted
	r.Sent += child.Sent
	r.Skipped += child.Skipped
	r.Failed += child.Failed
	if len(child.FailedRooms) > 0 {
		r.FailedRooms = append(r.FailedRooms, child.FailedRooms...)
	}
}

// 분산 락 / Room별 delivery claim TTL
const (
	DefaultExecutionLockTTL = 15 * time.Minute
	WeeklyDeliveryClaimTTL  = 8 * 24 * time.Hour  // 다음 주 새 weekKey 전까지 유효
	MonthlyDeliveryClaimTTL = 35 * 24 * time.Hour // 월간 재시도 기간 커버
)
