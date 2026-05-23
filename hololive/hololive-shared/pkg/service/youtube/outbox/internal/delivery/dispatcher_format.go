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

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/json"

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
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildVideoTemplateData(data, item)
	case domain.OutboxKindCommunityPost:
		return buildCommunityTemplateData(data, item.Payload)
	case domain.OutboxKindMilestone:
		return buildMilestoneTemplateData(data, item.Payload)
	}

	return data, nil
}

func buildVideoTemplateData(data templateData, item domain.YouTubeNotificationOutbox) (templateData, error) {
	var p videoPayload
	if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
		return data, fmt.Errorf("unmarshal video payload: %w", err)
	}
	data.Title = p.Title
	data.VideoID = p.VideoID
	data.URL = videoTemplateURL(item.Kind, p.VideoID)
	return data, nil
}

func videoTemplateURL(kind domain.OutboxKind, videoID string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf("https://www.youtube.com/shorts/%s", videoID)
	}
	return fmt.Sprintf("https://youtu.be/%s", videoID)
}

func buildCommunityTemplateData(data templateData, payload string) (templateData, error) {
	var p communityPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return data, fmt.Errorf("unmarshal community payload: %w", err)
	}
	data.ContentText = p.ContentText
	data.PostID = p.PostID
	data.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)
	return data, nil
}

func buildMilestoneTemplateData(data templateData, payload string) (templateData, error) {
	var p milestonePayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return data, fmt.Errorf("unmarshal milestone payload: %w", err)
	}
	data.Milestone = p.Milestone
	return data, nil
}

var singleMessageHeaderFormats = map[domain.OutboxKind]string{
	domain.OutboxKindCommunityPost: "📝 %s 커뮤니티 알림",
	domain.OutboxKindNewShort:      "📱 %s 쇼츠 알림",
	domain.OutboxKindNewVideo:      "📺 %s 새 영상",
	domain.OutboxKindLiveStream:    "📺 %s 방송 알림",
	domain.OutboxKindMilestone:     "🎉 %s 마일스톤 알림",
}

// applySeeMorePadding: 카카오톡 '전체 보기' 패딩 적용
// - COMMUNITY: 헤더 + 패딩 적용 (본문 길이 가변)
// - VIDEO, SHORTS, MILESTONE: 헤더만 추가, 패딩 미적용 (짧은 알림 → 즉시 노출)
func (mf *MessageFormatter) applySeeMorePadding(msg string, kind domain.OutboxKind, data templateData) string {
	header, ok := singleMessageHeader(kind, data.MemberName)
	if !ok {
		return msg
	}
	if kind == domain.OutboxKindCommunityPost {
		return util.ApplyKakaoSeeMorePadding(msg, header)
	}
	return header + "\n" + msg
}

func singleMessageHeader(kind domain.OutboxKind, memberName string) (string, bool) {
	format, ok := singleMessageHeaderFormats[kind]
	if !ok {
		return "", false
	}
	return fmt.Sprintf(format, memberName), true
}

func (mf *MessageFormatter) formatMessageFallback(memberName string, item domain.YouTubeNotificationOutbox) (string, error) {
	if item.Kind == domain.OutboxKindNewVideo || item.Kind == domain.OutboxKindNewShort || item.Kind == domain.OutboxKindLiveStream {
		return mf.formatVideoMessage(memberName, item.Payload, item.Kind)
	}
	if item.Kind == domain.OutboxKindCommunityPost {
		return mf.formatCommunityMessage(memberName, item.Payload)
	}
	if item.Kind == domain.OutboxKindMilestone {
		return mf.formatMilestoneMessage(memberName, item.Payload)
	}
	return "", fmt.Errorf("unknown outbox kind: %s", item.Kind)
}

// videoPayload: 영상 payload 구조
type videoPayload struct {
	CanonicalPostID  string     `json:"canonical_post_id,omitempty"`
	VideoID          string     `json:"video_id"`
	Title            string     `json:"title"`
	PublishedText    string     `json:"published_text,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	ScheduledStartAt *time.Time `json:"scheduled_start_at,omitempty"`
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
func (mf *MessageFormatter) formatVideoMessage(memberName, payload string, kind domain.OutboxKind) (string, error) {
	var p videoPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", fmt.Errorf("failed to unmarshal video payload: %w", err)
	}

	title := truncateString(p.Title, 50)
	if kind == domain.OutboxKindNewShort {
		url := fmt.Sprintf("https://www.youtube.com/shorts/%s", p.VideoID)
		header := fmt.Sprintf("📱 %s 쇼츠 알림", memberName)
		body := fmt.Sprintf("%s\n%s", title, url)
		return header + "\n" + body, nil
	}

	url := fmt.Sprintf("https://youtu.be/%s", p.VideoID)
	header := fmt.Sprintf("📺 %s 새 영상", memberName)
	if kind == domain.OutboxKindLiveStream {
		header = fmt.Sprintf("📺 %s 방송 알림", memberName)
	}
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
	config, ok := groupedTemplateConfigs[kind]
	if !ok {
		config = defaultGroupedTemplateConfig
	}
	return config.templateKey, fmt.Sprintf(config.headerFormat, memberName, count)
}

type groupedTemplateConfig struct {
	templateKey  domain.TemplateKey
	headerFormat string
}

var groupedTemplateConfigs = map[domain.OutboxKind]groupedTemplateConfig{
	domain.OutboxKindNewVideo:      {templateKey: domain.TemplateKeyOutboxVideoGroup, headerFormat: "📺 %s 새 영상 (%d개)"},
	domain.OutboxKindLiveStream:    {templateKey: domain.TemplateKeyOutboxVideoGroup, headerFormat: "📺 %s 방송 알림 (%d개)"},
	domain.OutboxKindNewShort:      {templateKey: domain.TemplateKeyOutboxShortsGroup, headerFormat: "📱 %s 쇼츠 알림 (%d개)"},
	domain.OutboxKindCommunityPost: {templateKey: domain.TemplateKeyOutboxCommunityGroup, headerFormat: "📝 %s 커뮤니티 알림 (%d개)"},
}

var defaultGroupedTemplateConfig = groupedTemplateConfig{
	templateKey:  domain.TemplateKeyOutboxVideoGroup,
	headerFormat: "🔔 %s 알림 (%d개)",
}

func (mf *MessageFormatter) buildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) groupedTemplateData {
	data := groupedTemplateData{
		MemberName: memberName,
		Count:      len(items),
		Items:      make([]groupedItemData, 0, len(items)),
	}

	for i := range items {
		data.Items = append(data.Items, buildGroupedItemData(items[i]))
	}

	return data
}

func buildGroupedItemData(item domain.YouTubeNotificationOutbox) groupedItemData {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildGroupedVideoItemData(item)
	case domain.OutboxKindCommunityPost:
		return buildGroupedCommunityItemData(item.Payload)
	default:
		return groupedItemData{}
	}
}

func buildGroupedVideoItemData(item domain.YouTubeNotificationOutbox) groupedItemData {
	var p videoPayload
	if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
		return groupedItemData{}
	}
	return groupedItemData{
		Title: p.Title,
		URL:   videoTemplateURL(item.Kind, p.VideoID),
	}
}

func buildGroupedCommunityItemData(payload string) groupedItemData {
	var p communityPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return groupedItemData{}
	}
	return groupedItemData{
		ContentText: p.ContentText,
		URL:         fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID),
	}
}
