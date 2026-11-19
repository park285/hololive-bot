package format

import (
	"cmp"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

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

func (mf *MessageFormatter) DisplayMemberName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}

	memberNameMissingTotal().Inc()

	return mf.MessageStrings.Text(messagestrings.MiscVTuberFallback)
}

type FormattedMessage struct {
	Message delivery.MessageVariants
	Err     error
}

func (mf *MessageFormatter) FormatMessages(ctx context.Context, items []domain.YouTubeNotificationOutbox) ([]FormattedMessage, error) {
	results := make([]FormattedMessage, len(items))
	requests := make([]template.RenderRequest, 0, 2*len(items))
	requestItems := make([]int, 0, len(items))
	memberNames := make(map[string]memberNameLookup)

	for i := range items {
		data, err := mf.messageTemplateData(ctx, &items[i], memberNames)
		if err != nil {
			results[i].Err = err

			continue
		}

		key := items[i].Kind.ToTemplateKey()

		requests = append(requests,
			template.RenderRequest{Key: key, ChannelID: items[i].ChannelID, Format: domain.TemplateFormatPlain, Data: data},
			template.RenderRequest{Key: key, ChannelID: items[i].ChannelID, Format: domain.TemplateFormatRichText, Data: data},
		)
		requestItems = append(requestItems, i)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("format messages: %w", err)
	}

	if len(requests) == 0 {
		return results, nil
	}

	if mf.Renderer == nil {
		return nil, errors.New("render templates: renderer is nil")
	}

	rendered, err := mf.Renderer.RenderBatch(ctx, requests)
	if err != nil {
		return nil, fmt.Errorf("render templates: %w", err)
	}

	for j, i := range requestItems {
		plain, richText := rendered[2*j], rendered[2*j+1]
		if err := cmp.Or(plain.Err, richText.Err); err != nil {
			results[i].Err = fmt.Errorf("render template %s: %w", requests[2*j].Key, err)

			continue
		}

		results[i].Message = delivery.MessageVariants{Plain: plain.Text, RichText: richText.Text}
	}

	return results, nil
}

type memberNameLookup struct {
	name string
	err  error
}

func (mf *MessageFormatter) messageTemplateData(ctx context.Context, item *domain.YouTubeNotificationOutbox, memberNames map[string]memberNameLookup) (TemplateData, error) {
	lookup, ok := memberNames[item.ChannelID]
	if !ok {
		lookup.name, lookup.err = mf.GetMemberName(ctx, item.ChannelID)
		memberNames[item.ChannelID] = lookup
	}

	if lookup.err != nil {
		return TemplateData{}, fmt.Errorf("get member name: %w", lookup.err)
	}

	data, err := mf.BuildTemplateData(mf.DisplayMemberName(lookup.name), item)
	if err != nil {
		return TemplateData{}, fmt.Errorf("build template data: %w", err)
	}

	return data, nil
}

func (mf *MessageFormatter) renderTemplate(ctx context.Context, templateKey domain.TemplateKey, channelID string, data any) (delivery.MessageVariants, error) {
	if mf.Renderer == nil {
		return delivery.MessageVariants{}, fmt.Errorf("render template %s: renderer is nil", templateKey)
	}

	results, err := mf.Renderer.RenderBatch(ctx, []template.RenderRequest{
		{Key: templateKey, ChannelID: channelID, Format: domain.TemplateFormatPlain, Data: data},
		{Key: templateKey, ChannelID: channelID, Format: domain.TemplateFormatRichText, Data: data},
	})
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render template %s: %w", templateKey, err)
	}

	if err := results[0].Err; err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render plain template %s: %w", templateKey, err)
	}

	if err := results[1].Err; err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render richtext template %s: %w", templateKey, err)
	}

	return delivery.MessageVariants{Plain: results[0].Text, RichText: results[1].Text}, nil
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

func (mf *MessageFormatter) FormatGroupedMessage(ctx context.Context, memberName, channelID string, kind domain.OutboxKind, items []domain.YouTubeNotificationOutbox) (delivery.MessageVariants, error) {
	if len(items) == 0 {
		return delivery.MessageVariants{}, errors.New("no items to format")
	}

	templateKey, err := groupedTemplateKey(kind)
	if err != nil {
		return delivery.MessageVariants{}, err
	}

	data, err := mf.BuildGroupedTemplateData(memberName, kind, items)
	if err != nil {
		return delivery.MessageVariants{}, err
	}

	return mf.renderTemplate(ctx, templateKey, channelID, data)
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
