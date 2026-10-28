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

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/llm/notification"
)

// 운영 CLI(hololive-api --rerun-notification=<kind>)가 다시 실행할 수 있는 정기 알림이다.
const (
	NotificationMajorEventWeekly  = "majorevent-weekly"
	NotificationMajorEventMonthly = "majorevent-monthly"
	NotificationMemberNewsWeekly  = "membernews-weekly"
)

// ErrUnknownNotification은 지원하지 않는 kind를 받았다는 뜻이다.
var ErrUnknownNotification = errors.New("unknown notification kind")

// RerunNotification은 정기 실행과 같은 scheduler 경로로 이번 주·월 알림을 한 번 적재한다. 같은 주·월 key의
// lock을 정기 실행과 공유하므로 실행 중이면 notification.ErrNotificationInProgress를 돌려주고 새로 적재하지 않는다.
// 이미 적재된 durable intent는 멱등하게 Queued로 센다.
func (r *LLMSchedulerRuntime) RerunNotification(ctx context.Context, kind string) (notification.Result, error) {
	switch kind {
	case NotificationMajorEventWeekly:
		if r.MajorEventScheduler == nil {
			return notification.Result{}, errors.New("rerun notification: major event weekly scheduler is not configured")
		}

		return wrapRerun(r.MajorEventScheduler.SendWeeklyNotification(ctx))
	case NotificationMajorEventMonthly:
		if r.MajorEventMonthlyScheduler == nil {
			return notification.Result{}, errors.New("rerun notification: major event monthly scheduler is not configured")
		}

		return wrapRerun(r.MajorEventMonthlyScheduler.SendMonthlyNotification(ctx))
	case NotificationMemberNewsWeekly:
		if r.MemberNewsScheduler == nil {
			return notification.Result{}, errors.New("rerun notification: member news weekly scheduler is not configured")
		}

		return wrapRerun(r.MemberNewsScheduler.SendWeeklyDigest(ctx))
	default:
		return notification.Result{}, fmt.Errorf("rerun notification: %w: %q", ErrUnknownNotification, kind)
	}
}

func wrapRerun(result notification.Result, err error) (notification.Result, error) {
	if err != nil {
		return result, fmt.Errorf("rerun notification: %w", err)
	}

	return result, nil
}
