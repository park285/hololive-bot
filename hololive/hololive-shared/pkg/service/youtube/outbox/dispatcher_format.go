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

package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

type MessageFormatter struct {
	renderer *template.Renderer
	cache    cache.Client
	logger   *slog.Logger
}

func (mf *MessageFormatter) formatMessage(ctx context.Context, item domain.YouTubeNotificationOutbox) (string, error) {
	memberName, err := mf.getMemberName(ctx, item.ChannelID)
	if err != nil {
		mf.logger.Warn("Failed to get member name, using fallback",
			slog.String("channel_id", item.ChannelID),
			slog.Any("error", err))
		memberName = "VTuber"
	} else if memberName == "" {
		memberName = "VTuber"
	}

	templateKey := item.Kind.ToTemplateKey()
	data, err := mf.buildTemplateData(memberName, item)
	if err != nil {
		return "", err
	}

	if mf.renderer != nil {
		msg, renderErr := mf.renderer.Render(ctx, templateKey, item.ChannelID, data)
		if renderErr == nil {
			return mf.applySeeMorePadding(msg, item.Kind, data), nil
		}
		mf.logger.Warn("Template render failed, using fallback",
			slog.String("template_key", string(templateKey)),
			slog.Any("error", renderErr))
	}

	return mf.formatMessageFallback(memberName, item)
}

type templateData struct {
	MemberName  string
	Title       string
	URL         string
	ContentText string
	Milestone   string
	VideoID     string
	PostID      string
}

func (mf *MessageFormatter) buildTemplateData(memberName string, item domain.YouTubeNotificationOutbox) (templateData, error) {
	data := templateData{MemberName: memberName}

	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var p videoPayload
		if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
			return data, fmt.Errorf("unmarshal video payload: %w", err)
		}
		data.Title = p.Title
		data.VideoID = p.VideoID
		if item.Kind == domain.OutboxKindNewShort {
			data.URL = fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
		} else {
			data.URL = fmt.Sprintf("https://youtu.be/%s", p.VideoID)
		}

	case domain.OutboxKindCommunityPost:
		var p communityPayload
		if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
			return data, fmt.Errorf("unmarshal community payload: %w", err)
		}
		data.ContentText = p.ContentText
		data.PostID = p.PostID
		data.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)

	case domain.OutboxKindMilestone:
		var p milestonePayload
		if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
			return data, fmt.Errorf("unmarshal milestone payload: %w", err)
		}
		data.Milestone = p.Milestone
	}

	return data, nil
}

// applySeeMorePadding: 카카오톡 '전체 보기' 패딩 적용
// - COMMUNITY: 헤더 + 패딩 적용 (본문 길이 가변)
// - VIDEO, SHORTS, MILESTONE: 헤더만 추가, 패딩 미적용 (짧은 알림 → 즉시 노출)
func (mf *MessageFormatter) applySeeMorePadding(msg string, kind domain.OutboxKind, data templateData) string {
	switch kind {
	case domain.OutboxKindCommunityPost:
		header := fmt.Sprintf("📝 %s 커뮤니티 알림", data.MemberName)
		return util.ApplyKakaoSeeMorePadding(msg, header)
	case domain.OutboxKindNewShort:
		header := fmt.Sprintf("📱 %s 쇼츠 알림", data.MemberName)
		return header + "\n" + msg
	case domain.OutboxKindNewVideo:
		header := fmt.Sprintf("📺 %s 새 영상", data.MemberName)
		return header + "\n" + msg
	case domain.OutboxKindMilestone:
		header := fmt.Sprintf("🎉 %s 마일스톤 알림", data.MemberName)
		return header + "\n" + msg
	default:
		return msg
	}
}

func (mf *MessageFormatter) formatMessageFallback(memberName string, item domain.YouTubeNotificationOutbox) (string, error) {
	switch item.Kind {
	case domain.OutboxKindNewVideo:
		return mf.formatVideoMessage(memberName, item.Payload, false)
	case domain.OutboxKindNewShort:
		return mf.formatVideoMessage(memberName, item.Payload, true)
	case domain.OutboxKindCommunityPost:
		return mf.formatCommunityMessage(memberName, item.Payload)
	case domain.OutboxKindMilestone:
		return mf.formatMilestoneMessage(memberName, item.Payload)
	default:
		return "", fmt.Errorf("unknown outbox kind: %s", item.Kind)
	}
}

// videoPayload: 영상 payload 구조
type videoPayload struct {
	CanonicalPostID string     `json:"canonical_post_id,omitempty"`
	VideoID         string     `json:"video_id"`
	Title           string     `json:"title"`
	PublishedText   string     `json:"published_text,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

// communityPayload: 커뮤니티 payload 구조
type communityPayload struct {
	CanonicalPostID string     `json:"canonical_post_id,omitempty"`
	PostID          string     `json:"post_id"`
	ContentText     string     `json:"content_text"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

// milestonePayload: 마일스톤 payload 구조
type milestonePayload struct {
	SubscriberCount int64  `json:"subscriber_count"`
	Milestone       string `json:"milestone"`
}

// formatVideoMessage: 영상 알림 메시지
func (mf *MessageFormatter) formatVideoMessage(memberName, payload string, isShort bool) (string, error) {
	var p videoPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal video payload: %w", err)
	}

	title := truncateString(p.Title, 50)
	if isShort {
		url := fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
		header := fmt.Sprintf("📱 %s 쇼츠 알림", memberName)
		body := fmt.Sprintf("%s\n%s", title, url)
		return header + "\n" + body, nil
	}

	url := fmt.Sprintf("https://youtu.be/%s", p.VideoID)
	header := fmt.Sprintf("📺 %s 새 영상", memberName)
	body := fmt.Sprintf("%s\n%s", title, url)
	return header + "\n" + body, nil
}

// formatCommunityMessage: 커뮤니티 포스트 알림 메시지
func (mf *MessageFormatter) formatCommunityMessage(memberName, payload string) (string, error) {
	var p communityPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal community payload: %w", err)
	}

	url := fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)
	header := fmt.Sprintf("📝 %s 커뮤니티 알림", memberName)
	body := fmt.Sprintf("%s\n\n%s", p.ContentText, url)
	return util.ApplyKakaoSeeMorePadding(body, header), nil
}

// formatMilestoneMessage: 마일스톤 알림 메시지
func (mf *MessageFormatter) formatMilestoneMessage(memberName, payload string) (string, error) {
	var p milestonePayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal milestone payload: %w", err)
	}

	header := fmt.Sprintf("🎉 %s 마일스톤 알림", memberName)
	body := fmt.Sprintf("%s %s 돌파!", memberName, p.Milestone)
	return header + "\n" + body, nil
}

// getMemberName: 멤버 이름 조회 (캐시)
func (mf *MessageFormatter) getMemberName(ctx context.Context, channelID string) (string, error) {
	name, err := mf.cache.HGet(ctx, "alarm:member_names", channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}
	return name, nil
}

type groupedItemData struct {
	Title       string
	ContentText string
	URL         string
}

type groupedTemplateData struct {
	MemberName string
	Count      int
	Items      []groupedItemData
}

func (mf *MessageFormatter) formatGroupedMessage(ctx context.Context, memberName, channelID string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to format")
	}
	if mf.renderer == nil {
		return "", fmt.Errorf("renderer is nil")
	}

	templateKey, header := mf.getGroupedTemplateKeyAndHeader(memberName, kind, len(items))
	data := mf.buildGroupedTemplateData(memberName, kind, items)

	msg, err := mf.renderer.Render(ctx, templateKey, channelID, data)
	if err != nil {
		return "", fmt.Errorf("render grouped template %s: %w", templateKey, err)
	}
	return util.ApplyKakaoSeeMorePadding(msg, header), nil
}

func (mf *MessageFormatter) getGroupedTemplateKeyAndHeader(memberName string, kind domain.OutboxKind, count int) (domain.TemplateKey, string) {
	switch kind {
	case domain.OutboxKindNewVideo:
		return domain.TemplateKeyOutboxVideoGroup, fmt.Sprintf("📺 %s 새 영상 (%d개)", memberName, count)
	case domain.OutboxKindNewShort:
		return domain.TemplateKeyOutboxShortsGroup, fmt.Sprintf("📱 %s 쇼츠 알림 (%d개)", memberName, count)
	case domain.OutboxKindCommunityPost:
		return domain.TemplateKeyOutboxCommunityGroup, fmt.Sprintf("📝 %s 커뮤니티 알림 (%d개)", memberName, count)
	default:
		return domain.TemplateKeyOutboxVideoGroup, fmt.Sprintf("🔔 %s 알림 (%d개)", memberName, count)
	}
}

func (mf *MessageFormatter) buildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) groupedTemplateData {
	data := groupedTemplateData{
		MemberName: memberName,
		Count:      len(items),
		Items:      make([]groupedItemData, 0, len(items)),
	}

	for i := range items {
		item := &items[i]
		itemData := groupedItemData{}

		switch item.Kind {
		case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
			var p videoPayload
			if err := json.Unmarshal([]byte(item.Payload), &p); err == nil {
				itemData.Title = p.Title
				if item.Kind == domain.OutboxKindNewShort {
					itemData.URL = fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
				} else {
					itemData.URL = fmt.Sprintf("https://youtu.be/%s", p.VideoID)
				}
			}
		case domain.OutboxKindCommunityPost:
			var p communityPayload
			if err := json.Unmarshal([]byte(item.Payload), &p); err == nil {
				itemData.ContentText = p.ContentText
				itemData.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)
			}
		}

		data.Items = append(data.Items, itemData)
	}

	return data
}
