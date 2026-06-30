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

package formatter

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

type liveStreamView struct {
	ChannelName string
	Title       string
	URL         string
	ViewerCount int
}

const streamListDisplayLimit = 100

type liveStreamsTemplateData struct {
	Count   int
	Streams []liveStreamView
}

type upcomingStreamView struct {
	ChannelName string
	Title       string
	TimeInfo    string
	URL         string
}

type upcomingStreamsTemplateData struct {
	Count   int
	Hours   int
	Streams []upcomingStreamView
}

type scheduleEntryView struct {
	IsLive   bool
	Title    string
	TimeInfo string
	URL      string
}

type channelScheduleTemplateData struct {
	ChannelName string
	Days        int
	Count       int
	Streams     []scheduleEntryView
}

func (f *ResponseFormatter) FormatLiveStreams(ctx context.Context, streams []*domain.Stream) string {
	data := f.liveStreamsTemplateData(ctx, streams)
	rendered, err := f.render(ctx, domain.TemplateKeyCmdLiveStreams, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) liveStreamsTemplateData(ctx context.Context, streams []*domain.Stream) liveStreamsTemplateData {
	return liveStreamsTemplateData{
		Count:   len(streams),
		Streams: f.liveStreamViews(ctx, streams),
	}
}

func (f *ResponseFormatter) liveStreamViews(ctx context.Context, streams []*domain.Stream) []liveStreamView {
	streams = limitedStreamList(streams)
	if len(streams) == 0 {
		return nil
	}

	views := make([]liveStreamView, len(streams))
	for i, stream := range streams {
		views[i] = f.liveStreamView(ctx, stream)
	}
	return views
}

func (f *ResponseFormatter) liveStreamView(ctx context.Context, stream *domain.Stream) liveStreamView {
	viewerCount := 0
	if stream.ViewerCount != nil {
		viewerCount = *stream.ViewerCount
	}

	return liveStreamView{
		ChannelName: f.formatChannelName(ctx, stream),
		Title:       f.truncateTitle(stream.Title),
		URL:         stream.GetYouTubeURL(),
		ViewerCount: viewerCount,
	}
}

func (f *ResponseFormatter) UpcomingStreams(ctx context.Context, streams []*domain.Stream, hours int) string {
	data := upcomingStreamsTemplateData{Count: len(streams), Hours: hours}
	streams = limitedStreamList(streams)
	if len(streams) > 0 {
		data.Streams = make([]upcomingStreamView, len(streams))
		for i, stream := range streams {
			data.Streams[i] = upcomingStreamView{
				ChannelName: f.formatChannelName(ctx, stream),
				Title:       f.truncateTitle(stream.Title),
				TimeInfo:    f.streamTimeInfo(ctx, stream),
				URL:         stream.GetYouTubeURL(),
			}
		}
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdUpcomingStreams, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func limitedStreamList(streams []*domain.Stream) []*domain.Stream {
	if len(streams) <= streamListDisplayLimit {
		return streams
	}

	return streams[:streamListDisplayLimit]
}

func (f *ResponseFormatter) ChannelSchedule(ctx context.Context, channel *domain.Channel, streams []*domain.Stream, days int) string {
	data := f.channelScheduleTemplateData(ctx, channel, streams, days)
	rendered, err := f.render(ctx, domain.TemplateKeyCmdChannelSchedule, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
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
	return channel.GetDisplayName()
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
	entry := scheduleEntryView{
		Title: f.truncateTitle(stream.Title),
		URL:   stream.GetYouTubeURL(),
	}
	if stream.IsLive() {
		entry.IsLive = true
		return entry
	}

	entry.TimeInfo = f.streamTimeInfo(ctx, stream)
	return entry
}

func (f *ResponseFormatter) truncateTitle(title string) string {
	return stringutil.TruncateString(title, constants.StringLimits.StreamTitle)
}

func (f *ResponseFormatter) streamTimeInfo(ctx context.Context, stream *domain.Stream) string {
	if stream == nil || stream.StartScheduled == nil {
		return f.messageStrings.GetContext(ctx, messagestrings.NamespaceMisc, "time_unknown")
	}

	kstTime := util.FormatKST(*stream.StartScheduled, "01/02 15:04")
	minutesUntil := stream.MinutesUntilStart()

	if minutesUntil <= 0 {
		return kstTime
	}

	hoursUntil := minutesUntil / 60
	minutesRem := minutesUntil % 60

	switch {
	case hoursUntil > 24:
		daysUntil := hoursUntil / 24
		return fmt.Sprintf("%s (%d일 후)", kstTime, daysUntil)
	case hoursUntil > 0:
		return fmt.Sprintf("%s (%d시간 %d분 후)", kstTime, hoursUntil, minutesRem)
	default:
		return fmt.Sprintf("%s (%d분 후)", kstTime, minutesRem)
	}
}

func (f *ResponseFormatter) formatChannelName(ctx context.Context, stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	name := stream.ChannelName
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

func (f *ResponseFormatter) formatStreamOrg(ctx context.Context, org string) string {
	if org == "" {
		return ""
	}

	if label := f.messageStrings.GetContext(ctx, messagestrings.NamespaceOrg, org); label != "" {
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
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMemberNoUpcoming(ctx context.Context, memberName string, hours int) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNoUpcoming, memberNoUpcomingTemplateData{MemberName: memberName, Hours: hours})
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}
