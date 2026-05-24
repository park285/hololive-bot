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

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/internal/messaging"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

type liveStreamView struct {
	ChannelName string
	Title       string
	URL         string
	ViewerCount int
}

type liveStreamsTemplateData struct {
	Emoji   msging.UIEmoji
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
	Emoji   msging.UIEmoji
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
	Emoji       msging.UIEmoji
	ChannelName string
	Days        int
	Count       int
	Streams     []scheduleEntryView
}

func (f *ResponseFormatter) FormatLiveStreams(ctx context.Context, streams []*domain.Stream) string {
	data := f.liveStreamsTemplateData(streams)
	rendered, err := f.render(ctx, domain.TemplateKeyCmdLiveStreams, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayLiveStreamsFailed)
	}

	if data.Count == 0 {
		return rendered
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}

	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func (f *ResponseFormatter) liveStreamsTemplateData(streams []*domain.Stream) liveStreamsTemplateData {
	return liveStreamsTemplateData{
		Emoji:   msging.DefaultEmoji,
		Count:   len(streams),
		Streams: f.liveStreamViews(streams),
	}
}

func (f *ResponseFormatter) liveStreamViews(streams []*domain.Stream) []liveStreamView {
	if len(streams) == 0 {
		return nil
	}

	views := make([]liveStreamView, len(streams))
	for i, stream := range streams {
		views[i] = f.liveStreamView(stream)
	}
	return views
}

func (f *ResponseFormatter) liveStreamView(stream *domain.Stream) liveStreamView {
	viewerCount := 0
	if stream.ViewerCount != nil {
		viewerCount = *stream.ViewerCount
	}

	return liveStreamView{
		ChannelName: f.formatChannelName(stream),
		Title:       f.truncateTitle(stream.Title),
		URL:         stream.GetYouTubeURL(),
		ViewerCount: viewerCount,
	}
}

func (f *ResponseFormatter) UpcomingStreams(ctx context.Context, streams []*domain.Stream, hours int) string {
	data := upcomingStreamsTemplateData{Emoji: msging.DefaultEmoji, Count: len(streams), Hours: hours}
	if len(streams) > 0 {
		data.Streams = make([]upcomingStreamView, len(streams))
		for i, stream := range streams {
			data.Streams[i] = upcomingStreamView{
				ChannelName: f.formatChannelName(stream),
				Title:       f.truncateTitle(stream.Title),
				TimeInfo:    f.streamTimeInfo(stream),
				URL:         stream.GetYouTubeURL(),
			}
		}
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdUpcomingStreams, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayUpcomingFailed)
	}

	if data.Count == 0 {
		return rendered
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}

	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func (f *ResponseFormatter) ChannelSchedule(ctx context.Context, channel *domain.Channel, streams []*domain.Stream, days int) string {
	data := f.channelScheduleTemplateData(channel, streams, days)
	rendered, err := f.render(ctx, domain.TemplateKeyCmdChannelSchedule, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayScheduleFailed)
	}

	if data.Count == 0 {
		return rendered
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}

	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func (f *ResponseFormatter) channelScheduleTemplateData(channel *domain.Channel, streams []*domain.Stream, days int) channelScheduleTemplateData {
	return channelScheduleTemplateData{
		Emoji:       msging.DefaultEmoji,
		ChannelName: channelScheduleName(channel),
		Days:        days,
		Count:       len(streams),
		Streams:     f.scheduleEntryViews(streams),
	}
}

func channelScheduleName(channel *domain.Channel) string {
	if channel == nil {
		return ""
	}
	return channel.GetDisplayName()
}

func (f *ResponseFormatter) scheduleEntryViews(streams []*domain.Stream) []scheduleEntryView {
	if len(streams) == 0 {
		return nil
	}

	entries := make([]scheduleEntryView, len(streams))
	for i, stream := range streams {
		entries[i] = f.scheduleEntryView(stream)
	}
	return entries
}

func (f *ResponseFormatter) scheduleEntryView(stream *domain.Stream) scheduleEntryView {
	entry := scheduleEntryView{
		Title: f.truncateTitle(stream.Title),
		URL:   stream.GetYouTubeURL(),
	}
	if stream.IsLive() {
		entry.IsLive = true
		return entry
	}

	entry.TimeInfo = f.streamTimeInfo(stream)
	return entry
}

func (f *ResponseFormatter) truncateTitle(title string) string {
	return stringutil.TruncateString(title, constants.StringLimits.StreamTitle)
}

func (f *ResponseFormatter) streamTimeInfo(stream *domain.Stream) string {
	if stream == nil || stream.StartScheduled == nil {
		return msging.MsgTimeUnknown
	}

	kstTime := util.FormatKST(*stream.StartScheduled, "01/02 15:04")
	minutesUntil := stream.MinutesUntilStart()

	if minutesUntil <= 0 {
		return kstTime
	}

	hoursUntil := minutesUntil / 60
	minutesRem := minutesUntil % 60

	if hoursUntil > 24 {
		daysUntil := hoursUntil / 24
		return fmt.Sprintf("%s (%d일 후)", kstTime, daysUntil)
	} else if hoursUntil > 0 {
		return fmt.Sprintf("%s (%d시간 %d분 후)", kstTime, hoursUntil, minutesRem)
	} else {
		return fmt.Sprintf("%s (%d분 후)", kstTime, minutesRem)
	}
}

func (f *ResponseFormatter) formatChannelName(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	name := stream.ChannelName
	displayOrg := streamDisplayOrg(stream)
	if displayOrg == "" {
		return name
	}

	return fmt.Sprintf("[%s] %s", displayOrg, name)
}

func streamDisplayOrg(stream *domain.Stream) string {
	if stream.Channel == nil || stream.Channel.Org == nil {
		return ""
	}
	return formatStreamOrg(*stream.Channel.Org)
}

func formatStreamOrg(org string) string {
	if org == "" {
		return ""
	}

	labels := map[string]string{
		"Hololive":     "Holo",
		"Nijisanji":    "니지산지",
		"Independents": "개인세",
		"Stellive":     "스텔라이브",
	}
	if label, ok := labels[org]; ok {
		return label
	}
	return org
}

func (f *ResponseFormatter) FormatMemberNotLive(memberName string) string {
	return fmt.Sprintf(msging.MsgMemberNotLive, memberName)
}

func (f *ResponseFormatter) FormatLiveOverflowCount(extraCount int) string {
	return fmt.Sprintf("\n\n외 %d개의 방송이 있습니다.", extraCount)
}

func (f *ResponseFormatter) FormatMemberNoUpcoming(memberName string, hours int) string {
	return fmt.Sprintf(msging.MsgMemberNoUpcoming, memberName, hours)
}

func (f *ResponseFormatter) FormatUpcomingOverflowCount(extraCount int) string {
	return fmt.Sprintf("\n\n외 %d개의 방송이 예정되어 있습니다.", extraCount)
}
