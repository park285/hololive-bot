package formatter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

type liveStreamView struct {
	ChannelName   string
	Title         string
	URL           string
	Elapsed       string
	CollabMembers string
}

const streamListDisplayLimit = 100

type liveStreamsTemplateData struct {
	Count   int
	Streams []liveStreamView
}

type upcomingStreamView struct {
	ChannelName   string
	Title         string
	TimeInfo      string
	URL           string
	CollabMembers string
}

type upcomingStreamsTemplateData struct {
	Count   int
	Hours   int
	Streams []upcomingStreamView
}

type scheduleEntryView struct {
	IsLive        bool
	Title         string
	TimeInfo      string
	URL           string
	CollabMembers string
}

type channelScheduleTemplateData struct {
	ChannelName string
	Days        int
	Count       int
	Streams     []scheduleEntryView
}

func (f *ResponseFormatter) renderLiveStreams(ctx context.Context, streams []*domain.Stream, asOf time.Time) (string, bool) {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdLiveStreams, f.liveStreamsTemplateData(ctx, streams, asOf))
	if err != nil {
		return "", false
	}

	return rendered, true
}

func (f *ResponseFormatter) liveStreamsTemplateData(ctx context.Context, streams []*domain.Stream, asOf time.Time) liveStreamsTemplateData {
	return liveStreamsTemplateData{
		Count:   len(streams),
		Streams: f.liveStreamViews(ctx, streams, asOf),
	}
}

func (f *ResponseFormatter) liveStreamViews(ctx context.Context, streams []*domain.Stream, asOf time.Time) []liveStreamView {
	streams = limitedStreamList(streams)
	if len(streams) == 0 {
		return nil
	}

	views := make([]liveStreamView, len(streams))
	for i, stream := range streams {
		views[i] = f.liveStreamView(ctx, stream, asOf)
	}

	return views
}

func (f *ResponseFormatter) liveStreamView(ctx context.Context, stream *domain.Stream, asOf time.Time) liveStreamView {
	return liveStreamView{
		ChannelName:   f.formatChannelName(ctx, stream),
		Title:         f.truncateTitle(stream.Title),
		URL:           stream.GetYouTubeURL(),
		Elapsed:       liveElapsed(stream.StartActual, asOf),
		CollabMembers: officialidentity.Format(stream.CollaboTalentNames),
	}
}

func liveElapsed(startedAt *time.Time, asOf time.Time) string {
	if startedAt == nil || asOf.IsZero() || startedAt.After(asOf) {
		return ""
	}

	elapsed := asOf.Sub(*startedAt)
	if elapsed < time.Minute {
		return "방금 시작"
	}

	return formatBroadcastDuration(elapsed) + "째"
}

func (f *ResponseFormatter) UpcomingStreams(ctx context.Context, streams []*domain.Stream, hours int) string {
	data := upcomingStreamsTemplateData{Count: len(streams), Hours: hours}

	streams = limitedStreamList(streams)

	if len(streams) > 0 {
		data.Streams = make([]upcomingStreamView, len(streams))
		for i, stream := range streams {
			data.Streams[i] = upcomingStreamView{
				ChannelName:   f.formatChannelName(ctx, stream),
				Title:         f.truncateTitle(stream.Title),
				TimeInfo:      f.streamTimeInfo(ctx, stream),
				URL:           stream.GetYouTubeURL(),
				CollabMembers: officialidentity.Format(stream.CollaboTalentNames),
			}
		}
	}

	return f.renderResponse(ctx, domain.TemplateKeyCmdUpcomingStreams, data, templateview.ShouldFoldItems(len(data.Streams)))
}

func limitedStreamList(streams []*domain.Stream) []*domain.Stream {
	if len(streams) <= streamListDisplayLimit {
		return streams
	}

	return streams[:streamListDisplayLimit]
}

func (f *ResponseFormatter) foldStreamList(rendered string, streams []*domain.Stream) string {
	return f.foldSeeMore(rendered, templateview.ShouldFoldItems(len(limitedStreamList(streams))))
}

func (f *ResponseFormatter) ChannelSchedule(ctx context.Context, channel *domain.Channel, streams []*domain.Stream, days int) string {
	data := f.channelScheduleTemplateData(ctx, channel, streams, days)

	return f.renderResponse(ctx, domain.TemplateKeyCmdChannelSchedule, data, data.ChannelName != "" && templateview.ShouldFoldItems(len(data.Streams)))
}

func (f *ResponseFormatter) channelScheduleTemplateData(ctx context.Context, channel *domain.Channel, streams []*domain.Stream, days int) channelScheduleTemplateData {
	return channelScheduleTemplateData{
		ChannelName: channelScheduleName(channel),
		Days:        days,
		Count:       len(streams),
		Streams:     f.scheduleEntryViews(ctx, streams),
	}
}

func channelScheduleName(channel *domain.Channel) string {
	if channel == nil {
		return ""
	}

	return channel.Name
}

func (f *ResponseFormatter) scheduleEntryViews(ctx context.Context, streams []*domain.Stream) []scheduleEntryView {
	streams = limitedStreamList(streams)
	if len(streams) == 0 {
		return nil
	}

	entries := make([]scheduleEntryView, len(streams))
	for i, stream := range streams {
		entries[i] = f.scheduleEntryView(ctx, stream)
	}

	return entries
}

func (f *ResponseFormatter) scheduleEntryView(ctx context.Context, stream *domain.Stream) scheduleEntryView {
	title := stream.Title
	if label := mekparkhost.Identify(stream.ChannelID, title).Label(); label != "" {
		title = label + " · " + title
	}

	entry := scheduleEntryView{
		Title:         f.truncateTitle(title),
		URL:           stream.GetYouTubeURL(),
		CollabMembers: officialidentity.Format(stream.CollaboTalentNames),
	}
	if stream.IsLive() {
		entry.IsLive = true
		return entry
	}

	entry.TimeInfo = f.streamTimeInfo(ctx, stream)

	return entry
}

const streamTitleMaxRunes = 100

func (f *ResponseFormatter) truncateTitle(title string) string {
	title = strings.Join(strings.Fields(strings.ReplaceAll(title, kakaotext.ZeroWidthSpace, "")), " ")

	return stringutil.TruncateString(title, streamTitleMaxRunes)
}

func (f *ResponseFormatter) streamTimeInfo(_ context.Context, stream *domain.Stream) string {
	if stream == nil || stream.StartScheduled == nil {
		return f.messageStrings.Text(messagestrings.MiscTimeUnknown)
	}

	kstTime := timeutil.FormatKST(*stream.StartScheduled, "01/02 15:04")
	minutesUntil := stream.MinutesUntilStart()

	if minutesUntil <= 0 {
		return kstTime
	}

	hoursUntil := minutesUntil / 60
	minutesRem := minutesUntil % 60

	switch {
	case hoursUntil > 24:
		daysUntil := hoursUntil / 24
		return fmt.Sprintf(f.messageStrings.Text(messagestrings.TimeFmtStreamTimeDays), kstTime, daysUntil)
	case hoursUntil > 0:
		return fmt.Sprintf(f.messageStrings.Text(messagestrings.TimeFmtStreamTimeHoursMinutes), kstTime, hoursUntil, minutesRem)
	default:
		return fmt.Sprintf(f.messageStrings.Text(messagestrings.TimeFmtStreamTimeMinutes), kstTime, minutesRem)
	}
}

func (f *ResponseFormatter) formatChannelName(ctx context.Context, stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	name := mekparkhost.DisplayName(stream.ChannelID, stream.Title, stream.ChannelName)

	displayOrg := f.streamDisplayOrg(ctx, stream)

	if displayOrg == "" {
		return name
	}

	return fmt.Sprintf("[%s] %s", displayOrg, name)
}

func (f *ResponseFormatter) streamDisplayOrg(ctx context.Context, stream *domain.Stream) string {
	if stream.Channel == nil || stream.Channel.Org == nil {
		return ""
	}

	return f.formatStreamOrg(ctx, *stream.Channel.Org)
}

func (f *ResponseFormatter) formatStreamOrg(_ context.Context, org string) string {
	if org == "" || org == constants.HolodexAPIParams.OrgHololive {
		return ""
	}

	if label, ok := f.messageStrings.Lookup(messagestrings.NamespaceOrg, org); ok {
		return label
	}

	return org
}

type memberNotLiveTemplateData struct {
	MemberName string
}

type memberNoUpcomingTemplateData struct {
	MemberName string
	Hours      int
}

func (f *ResponseFormatter) FormatMemberNotLive(ctx context.Context, memberName string) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNotLive, memberNotLiveTemplateData{MemberName: memberName})
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}

func (f *ResponseFormatter) FormatMemberNoUpcoming(ctx context.Context, memberName string, hours int) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNoUpcoming, memberNoUpcomingTemplateData{MemberName: memberName, Hours: hours})
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
