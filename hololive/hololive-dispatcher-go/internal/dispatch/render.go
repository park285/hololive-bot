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

package dispatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// Renderer: 알림 그룹을 사용자 메시지로 렌더링한다.
type Renderer interface {
	RenderGroup(ctx context.Context, group NotificationGroup) (string, error)
}

// SimpleRenderer: 기본 문자열 포맷 렌더러.
type SimpleRenderer struct{}

// NewSimpleRenderer: 기본 렌더러 생성.
func NewSimpleRenderer() *SimpleRenderer {
	return &SimpleRenderer{}
}

// RenderGroup: 그룹 메시지를 렌더링한다.
func (r *SimpleRenderer) RenderGroup(_ context.Context, group NotificationGroup) (string, error) {
	if len(group.Notifications) == 0 {
		return "", fmt.Errorf("render group: notifications is empty")
	}

	if len(group.Notifications) == 1 {
		return renderNotification(group.Notifications[0]), nil
	}

	var builder strings.Builder
	if group.MinutesUntil <= 0 {
		builder.WriteString("🔔 방송 시작 알림\n")
	} else {
		fmt.Fprintf(&builder, "⏰ %d분 내 방송 알림\n", group.MinutesUntil)
	}

	for _, notification := range group.Notifications {
		builder.WriteString("- ")
		builder.WriteString(renderNotificationSummary(notification))
		builder.WriteString("\n")
	}

	return strings.TrimSpace(builder.String()), nil
}

func renderNotification(notification domain.AlarmNotification) string {
	memberName := resolveMemberName(notification)
	title := resolveTitle(notification)
	url := resolveStreamURL(notification)

	var builder strings.Builder
	if notification.MinutesUntil <= 0 {
		fmt.Fprintf(&builder, "🔔 %s 방송 시작!\n", memberName)
	} else {
		fmt.Fprintf(&builder, "⏰ %s 방송 %d분 전\n", memberName, notification.MinutesUntil)
	}
	fmt.Fprintf(&builder, "📺 %s\n", title)
	if scheduleMessage := strings.TrimSpace(notification.ScheduleChangeMessage); scheduleMessage != "" {
		fmt.Fprintf(&builder, "📅 %s\n", scheduleMessage)
	}
	if url != "" {
		fmt.Fprintf(&builder, "🔗 %s", url)
	}

	return strings.TrimSpace(builder.String())
}

func renderNotificationSummary(notification domain.AlarmNotification) string {
	memberName := resolveMemberName(notification)
	title := resolveTitle(notification)
	url := resolveStreamURL(notification)
	if url == "" {
		return fmt.Sprintf("%s - %s", memberName, title)
	}
	return fmt.Sprintf("%s - %s (%s)", memberName, title, url)
}

func resolveMemberName(notification domain.AlarmNotification) string {
	if notification.Channel != nil && strings.TrimSpace(notification.Channel.Name) != "" {
		return strings.TrimSpace(notification.Channel.Name)
	}
	if notification.Stream != nil && strings.TrimSpace(notification.Stream.ChannelName) != "" {
		return strings.TrimSpace(notification.Stream.ChannelName)
	}
	return "알 수 없는 멤버"
}

func resolveTitle(notification domain.AlarmNotification) string {
	if notification.Stream == nil {
		return "방송 정보 없음"
	}
	if title := strings.TrimSpace(notification.Stream.Title); title != "" {
		return title
	}
	return "제목 없음"
}

func resolveStreamURL(notification domain.AlarmNotification) string {
	if notification.Stream == nil {
		return ""
	}

	stream := notification.Stream
	switch {
	case stream.IsTwitchOnly && stream.GetTwitchLiveURL() != "":
		return stream.GetTwitchLiveURL()
	case stream.IsChzzkOnly && stream.GetChzzkLiveURL() != "":
		return stream.GetChzzkLiveURL()
	case stream.IsIntegrated && stream.GetYouTubeURL() != "":
		if chzzkURL := stream.GetChzzkLiveURL(); chzzkURL != "" {
			return fmt.Sprintf("%s | %s", stream.GetYouTubeURL(), chzzkURL)
		}
		return stream.GetYouTubeURL()
	default:
		return stream.GetYouTubeURL()
	}
}
