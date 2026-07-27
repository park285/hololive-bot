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

package format

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type MessageFormatter struct {
	Renderer       *template.Renderer
	Cache          cache.Client
	Logger         *slog.Logger
	MessageStrings *messagestrings.Store
}

func NewMessageFormatter(renderer *template.Renderer, cacheClient cache.Client, logger *slog.Logger, messageStrings *messagestrings.Store) *MessageFormatter {
	if logger == nil {
		logger = slog.Default()
	}
	return &MessageFormatter{Renderer: renderer, Cache: cacheClient, Logger: logger, MessageStrings: messageStrings}
}

func (mf *MessageFormatter) logger() *slog.Logger {
	if mf.Logger != nil {
		return mf.Logger
	}
	return slog.Default()
}

func (mf *MessageFormatter) FormatMessage(ctx context.Context, item *domain.YouTubeNotificationOutbox) (string, error) {
	if item == nil {
		return "", fmt.Errorf("notification outbox item is nil")
	}
	memberName, err := mf.GetMemberName(ctx, item.ChannelID)
	if err != nil {
		mf.logger().Warn("Failed to get member name, using fallback",
			slog.String("channel_id", item.ChannelID),
			slog.Any("error", err))
		memberName = mf.MessageStrings.VTuberFallbackContext(ctx)
	} else if memberName == "" {
		memberName = mf.MessageStrings.VTuberFallbackContext(ctx)
	}

	data, err := mf.BuildTemplateData(memberName, item)
	if err != nil {
		return "", err
	}

	return mf.renderTemplate(ctx, item.Kind.ToTemplateKey(), item.ChannelID, data)
}

func (mf *MessageFormatter) renderTemplate(ctx context.Context, templateKey domain.TemplateKey, channelID string, data any) (string, error) {
	if mf.Renderer == nil {
		return "", fmt.Errorf("render template %s: renderer is nil", templateKey)
	}
	msg, err := mf.Renderer.Render(ctx, templateKey, channelID, data)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", templateKey, err)
	}
	return msg, nil
}

type TemplateData struct {
	MemberName  string
	Kind        string
	Title       string
	URL         string
	ContentText string
	Milestone   string
	VideoID     string
	PostID      string
}

func (mf *MessageFormatter) BuildTemplateData(memberName string, item *domain.YouTubeNotificationOutbox) (TemplateData, error) {
	data := TemplateData{MemberName: memberName, Kind: string(item.Kind)}
	if err := populateTemplateData(&data, item); err != nil {
		return TemplateData{}, err
	}
	return data, nil
}

func populateTemplateData(data *TemplateData, item *domain.YouTubeNotificationOutbox) error {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildVideoTemplateData(data, item)
	case domain.OutboxKindCommunityPost:
		return buildCommunityTemplateData(data, item.Payload)
	case domain.OutboxKindMilestone:
		return buildMilestoneTemplateData(data, item.Payload)
	default:
		return nil
	}
}

func buildVideoTemplateData(data *TemplateData, item *domain.YouTubeNotificationOutbox) error {
	var p VideoPayload
	if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
		return fmt.Errorf("unmarshal video payload: %w", err)
	}
	data.Title = p.Title
	data.VideoID = p.VideoID
	data.URL = VideoTemplateURL(item.Kind, p.VideoID)
	return nil
}

func VideoTemplateURL(kind domain.OutboxKind, videoID string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf("https://www.youtube.com/shorts/%s", videoID)
	}
	return fmt.Sprintf("https://youtu.be/%s", videoID)
}

func buildCommunityTemplateData(data *TemplateData, payload string) error {
	var p CommunityPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("unmarshal community payload: %w", err)
	}
	data.ContentText = p.ContentText
	data.PostID = p.PostID
	data.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)
	return nil
}

func buildMilestoneTemplateData(data *TemplateData, payload string) error {
	var p MilestonePayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("unmarshal milestone payload: %w", err)
	}
	data.Milestone = p.Milestone
	return nil
}

type VideoPayload struct {
	CanonicalPostID  string     `json:"canonical_post_id,omitempty"`
	VideoID          string     `json:"video_id"`
	Title            string     `json:"title"`
	PublishedText    string     `json:"published_text,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	ScheduledStartAt *time.Time `json:"scheduled_start_at,omitempty"`
}

type CommunityPayload struct {
	CanonicalPostID string     `json:"canonical_post_id,omitempty"`
	PostID          string     `json:"post_id"`
	ContentText     string     `json:"content_text"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

type MilestonePayload struct {
	SubscriberCount int64  `json:"subscriber_count"`
	Milestone       string `json:"milestone"`
}

func (mf *MessageFormatter) GetMemberName(ctx context.Context, channelID string) (string, error) {
	if mf.Cache == nil {
		return "", fmt.Errorf("cache client is nil")
	}
	name, err := mf.Cache.HGet(ctx, "alarm:member_names", channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}
	return name, nil
}

type GroupedItemData struct {
	Title       string
	ContentText string
	URL         string
}

type GroupedTemplateData struct {
	MemberName string
	Kind       string
	Count      int
	Items      []GroupedItemData
}

func (mf *MessageFormatter) FormatGroupedMessage(ctx context.Context, memberName, channelID string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to format")
	}
	data := mf.BuildGroupedTemplateData(memberName, kind, items)
	return mf.renderTemplate(ctx, groupedTemplateKey(kind), channelID, data)
}

func groupedTemplateKey(kind domain.OutboxKind) domain.TemplateKey {
	switch kind {
	case domain.OutboxKindNewShort:
		return domain.TemplateKeyOutboxShortsGroup
	case domain.OutboxKindCommunityPost:
		return domain.TemplateKeyOutboxCommunityGroup
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return domain.TemplateKeyOutboxVideoGroup
	default:
		return domain.TemplateKeyOutboxVideoGroup
	}
}

func (mf *MessageFormatter) BuildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) GroupedTemplateData {
	data := GroupedTemplateData{
		MemberName: memberName,
		Kind:       string(kind),
		Count:      len(items),
		Items:      make([]GroupedItemData, 0, len(items)),
	}

	for i := range items {
		data.Items = append(data.Items, BuildGroupedItemData(&items[i]))
	}

	return data
}

func BuildGroupedItemData(item *domain.YouTubeNotificationOutbox) GroupedItemData {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildGroupedVideoItemData(item)
	case domain.OutboxKindCommunityPost:
		return buildGroupedCommunityItemData(item.Payload)
	case domain.OutboxKindMilestone:
		return GroupedItemData{}
	default:
		return GroupedItemData{}
	}
}

func buildGroupedVideoItemData(item *domain.YouTubeNotificationOutbox) GroupedItemData {
	var p VideoPayload
	if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
		return GroupedItemData{}
	}
	return GroupedItemData{
		Title: p.Title,
		URL:   VideoTemplateURL(item.Kind, p.VideoID),
	}
}

func buildGroupedCommunityItemData(payload string) GroupedItemData {
	var p CommunityPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return GroupedItemData{}
	}
	return GroupedItemData{
		ContentText: p.ContentText,
		URL:         fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID),
	}
}
