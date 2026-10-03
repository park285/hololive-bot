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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

// MemberNameSource는 알림 표시명의 정본 조회다. 운영 구현은 PostgreSQL을 읽는 alarm.Repository.GetMemberName이며,
// members의 한국어 표시명(short_korean_name→korean_name)을 사용한다. 조회 오류는 오류로
// 돌려주고, 빈 문자열은 표시명이 없다는 뜻이다.
type MemberNameSource interface {
	GetMemberName(ctx context.Context, channelID string) (string, error)
}

type MessageFormatter struct {
	Renderer       *template.Renderer
	MemberNames    MemberNameSource
	MessageStrings *messagestrings.Store
}

func NewMessageFormatter(renderer *template.Renderer, memberNames MemberNameSource, messageStrings *messagestrings.Store) *MessageFormatter {
	return &MessageFormatter{Renderer: renderer, MemberNames: memberNames, MessageStrings: messageStrings}
}

// DisplayMemberName은 알림에 쓸 멤버 표시명을 돌려준다. 멤버 데이터(members)에 한국어 표시명이 없으면 멤버 표시명 예외 계약의
// 종단 문구(misc/vtuber_fallback)를 쓰고 hololive_youtube_outbox_member_name_missing_total로 센다.
func (mf *MessageFormatter) DisplayMemberName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}

	memberNameMissingTotal().Inc()

	return mf.MessageStrings.Text(messagestrings.MiscVTuberFallback)
}

func (mf *MessageFormatter) FormatMessage(ctx context.Context, item *domain.YouTubeNotificationOutbox) (string, error) {
	if item == nil {
		return "", errors.New("notification outbox item is nil")
	}

	// 조회 오류는 대체 문구로 바꾸지 않고 포맷 실패로 돌려준다. 호출자는 재시도 가능한 실패로 전이한다.
	memberName, err := mf.GetMemberName(ctx, item.ChannelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}

	memberName = mf.DisplayMemberName(memberName)

	data, err := mf.BuildTemplateData(memberName, item)
	if err != nil {
		return "", fmt.Errorf("build template data: %w", err)
	}

	out, err := mf.renderTemplate(ctx, item.Kind.ToTemplateKey(), item.ChannelID, data)
	if err != nil {
		return out, fmt.Errorf("render template: %w", err)
	}

	return out, nil
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
	MemberName           string
	Kind                 string
	Title                string
	URL                  string
	ContentText          string
	VideoID              string
	PostID               string
	IsPremiere           bool
	IsUpcomingPremiere   bool
	MinutesUntilPremiere int
}

func (mf *MessageFormatter) BuildTemplateData(memberName string, item *domain.YouTubeNotificationOutbox) (TemplateData, error) {
	data := TemplateData{MemberName: memberName, Kind: string(item.Kind)}
	if err := populateTemplateData(&data, item); err != nil {
		return TemplateData{}, fmt.Errorf("populate template data: %w", err)
	}

	return data, nil
}

func populateTemplateData(data *TemplateData, item *domain.YouTubeNotificationOutbox) error {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return populateVideoTemplateData(data, item)
	case domain.OutboxKindCommunityPost:
		return populateCommunityTemplateData(data, item.Payload)
	default:
		// 알 수 없는 kind를 빈 데이터로 영상 template에 렌더링하지 않는다. 렌더링 전에 포맷 실패로 드러낸다.
		return fmt.Errorf("unsupported outbox kind %q", item.Kind)
	}
}

func populateVideoTemplateData(data *TemplateData, item *domain.YouTubeNotificationOutbox) error {
	if err := buildVideoTemplateData(data, item); err != nil {
		return fmt.Errorf("build video template data: %w", err)
	}

	return nil
}

func populateCommunityTemplateData(data *TemplateData, payload string) error {
	if err := buildCommunityTemplateData(data, payload); err != nil {
		return fmt.Errorf("build community template data: %w", err)
	}

	return nil
}

func buildVideoTemplateData(data *TemplateData, item *domain.YouTubeNotificationOutbox) error {
	var p youtubeoutbox.Video

	if err := jsonv2.Unmarshal([]byte(item.Payload), &p); err != nil {
		return fmt.Errorf("unmarshal video payload: %w", err)
	}

	data.Title = p.Title
	data.MemberName = mekparkhost.DisplayName(item.ChannelID, p.Title, data.MemberName)
	data.VideoID = p.VideoID
	data.URL = VideoTemplateURL(item.Kind, p.VideoID)
	populatePremiereTemplateData(data, item.Kind, &p, time.Now())

	return nil
}

func populatePremiereTemplateData(data *TemplateData, kind domain.OutboxKind, payload *youtubeoutbox.Video, now time.Time) {
	if kind != domain.OutboxKindNewVideo || payload.IsPremiere == nil || !*payload.IsPremiere {
		return
	}

	data.IsPremiere = true

	if payload.ScheduledStartAt == nil || payload.ScheduledStartAt.IsZero() {
		return
	}

	data.MinutesUntilPremiere = timeutil.MinutesUntilCeilPtr(payload.ScheduledStartAt, now)
	data.IsUpcomingPremiere = data.MinutesUntilPremiere > 0
}

func VideoTemplateURL(kind domain.OutboxKind, videoID string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf("https://www.youtube.com/shorts/%s", videoID)
	}

	return fmt.Sprintf("https://youtu.be/%s", videoID)
}

func buildCommunityTemplateData(data *TemplateData, payload string) error {
	var p youtubeoutbox.Community

	if err := jsonv2.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("unmarshal community payload: %w", err)
	}

	data.ContentText = p.ContentText
	data.PostID = p.PostID
	data.URL = fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID)

	return nil
}

func (mf *MessageFormatter) GetMemberName(ctx context.Context, channelID string) (string, error) {
	if mf.MemberNames == nil {
		return "", errors.New("member name source is nil")
	}

	name, err := mf.MemberNames.GetMemberName(ctx, channelID)
	if err != nil {
		return "", fmt.Errorf("member name source: %w", err)
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
		return "", errors.New("no items to format")
	}

	templateKey, err := groupedTemplateKey(kind)
	if err != nil {
		return "", err
	}

	data, err := mf.BuildGroupedTemplateData(memberName, kind, items)
	if err != nil {
		return "", err
	}

	out, err := mf.renderTemplate(ctx, templateKey, channelID, data)
	if err != nil {
		return out, fmt.Errorf("render template: %w", err)
	}

	return out, nil
}

func groupedTemplateKey(kind domain.OutboxKind) (domain.TemplateKey, error) {
	switch kind {
	case domain.OutboxKindNewShort:
		return domain.TemplateKeyOutboxShortsGroup, nil
	case domain.OutboxKindCommunityPost:
		return domain.TemplateKeyOutboxCommunityGroup, nil
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream:
		return domain.TemplateKeyOutboxVideoGroup, nil
	default:
		return "", fmt.Errorf("grouped template: unsupported outbox kind %q", kind)
	}
}

// BuildGroupedTemplateData는 묶음 template 입력을 만든다. 항목 하나라도 payload를 읽지 못하면 빈 항목으로
// 보내지 않고 오류를 돌려주며, 호출자는 묶음 전체를 재시도 가능한 포맷 실패로 처리한다.
func (mf *MessageFormatter) BuildGroupedTemplateData(memberName string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (GroupedTemplateData, error) {
	data := GroupedTemplateData{
		MemberName: memberName,
		Kind:       string(kind),
		Count:      len(items),
		Items:      make([]GroupedItemData, 0, len(items)),
	}

	for i := range items {
		item, err := buildGroupedItemData(&items[i])
		if err != nil {
			return GroupedTemplateData{}, fmt.Errorf("grouped item %d (outbox %d): %w", i, items[i].ID, err)
		}

		data.Items = append(data.Items, item)
	}

	return data, nil
}

func buildGroupedItemData(item *domain.YouTubeNotificationOutbox) (GroupedItemData, error) {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildGroupedVideoItemData(item)
	case domain.OutboxKindCommunityPost:
		return buildGroupedCommunityItemData(item.Payload)
	default:
		return GroupedItemData{}, fmt.Errorf("unsupported outbox kind %q", item.Kind)
	}
}

func buildGroupedVideoItemData(item *domain.YouTubeNotificationOutbox) (GroupedItemData, error) {
	var p youtubeoutbox.Video

	if err := jsonv2.Unmarshal([]byte(item.Payload), &p); err != nil {
		return GroupedItemData{}, fmt.Errorf("unmarshal video payload: %w", err)
	}

	title := p.Title
	if label := mekparkhost.Identify(item.ChannelID, p.Title).Label(); label != "" {
		title = label + " · " + title
	}

	return GroupedItemData{
		Title: title,
		URL:   VideoTemplateURL(item.Kind, p.VideoID),
	}, nil
}

func buildGroupedCommunityItemData(payload string) (GroupedItemData, error) {
	var p youtubeoutbox.Community

	if err := jsonv2.Unmarshal([]byte(payload), &p); err != nil {
		return GroupedItemData{}, fmt.Errorf("unmarshal community payload: %w", err)
	}

	return GroupedItemData{
		ContentText: p.ContentText,
		URL:         fmt.Sprintf("https://www.youtube.com/post/%s", p.PostID),
	}, nil
}
