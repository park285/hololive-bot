package adapter

import (
	"context"
	"fmt"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

type liveStreamView struct {
	ChannelName string
	Title       string
	URL         string
	ViewerCount int
}

type liveStreamsTemplateData struct {
	Emoji   UIEmoji
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
	Emoji   UIEmoji
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
	Emoji       UIEmoji
	ChannelName string
	Days        int
	Count       int
	Streams     []scheduleEntryView
}

func (f *ResponseFormatter) FormatLiveStreams(ctx context.Context, streams []*domain.Stream) string {
	data := liveStreamsTemplateData{Emoji: DefaultEmoji, Count: len(streams)}
	if len(streams) > 0 {
		data.Streams = make([]liveStreamView, len(streams))
		for i, stream := range streams {
			viewerCount := 0
			if stream.ViewerCount != nil {
				viewerCount = *stream.ViewerCount
			}
			data.Streams[i] = liveStreamView{
				ChannelName: f.formatChannelName(stream),
				Title:       f.truncateTitle(stream.Title),
				URL:         stream.GetYouTubeURL(),
				ViewerCount: viewerCount,
			}
		}
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdLiveStreams, data)
	if err != nil {
		return ErrorMessage(ErrDisplayLiveStreamsFailed)
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

func (f *ResponseFormatter) UpcomingStreams(ctx context.Context, streams []*domain.Stream, hours int) string {
	data := upcomingStreamsTemplateData{Emoji: DefaultEmoji, Count: len(streams), Hours: hours}
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
		return ErrorMessage(ErrDisplayUpcomingFailed)
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
	data := channelScheduleTemplateData{Emoji: DefaultEmoji, Days: days, Count: len(streams)}
	if channel != nil {
		data.ChannelName = channel.GetDisplayName()
	}
	if len(streams) > 0 {
		data.Streams = make([]scheduleEntryView, len(streams))
		for i, stream := range streams {
			entry := scheduleEntryView{
				Title: f.truncateTitle(stream.Title),
				URL:   stream.GetYouTubeURL(),
			}

			if stream.IsLive() {
				entry.IsLive = true
			} else {
				entry.TimeInfo = f.streamTimeInfo(stream)
			}

			data.Streams[i] = entry
		}
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdChannelSchedule, data)
	if err != nil {
		return ErrorMessage(ErrDisplayScheduleFailed)
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

func (f *ResponseFormatter) truncateTitle(title string) string {
	return stringutil.TruncateString(title, constants.StringLimits.StreamTitle)
}

func (f *ResponseFormatter) streamTimeInfo(stream *domain.Stream) string {
	if stream == nil || stream.StartScheduled == nil {
		return MsgTimeUnknown
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

	if stream.Channel != nil && stream.Channel.Org != nil {
		org := *stream.Channel.Org
		if org != "" {
			displayOrg := org
			switch org {
			case "Hololive":
				displayOrg = "Holo"
			case "Nijisanji":
				displayOrg = "니지산지"
			case "VSPO":
				displayOrg = "VSPO"
			case "Indie":
				displayOrg = "개인세"
			case "Stellive":
				displayOrg = "스텔라이브"
			}
			name = fmt.Sprintf("[%s] %s", displayOrg, name)
		}
	}

	return name
}

func (f *ResponseFormatter) FormatMemberNotLive(memberName string) string {
	return fmt.Sprintf(MsgMemberNotLive, memberName)
}

func (f *ResponseFormatter) FormatLiveOverflowCount(extraCount int) string {
	return fmt.Sprintf("\n\n외 %d개의 방송이 있습니다.", extraCount)
}

func (f *ResponseFormatter) FormatMemberNoUpcoming(memberName string, hours int) string {
	return fmt.Sprintf(MsgMemberNoUpcoming, memberName, hours)
}

func (f *ResponseFormatter) FormatUpcomingOverflowCount(extraCount int) string {
	return fmt.Sprintf("\n\n외 %d개의 방송이 예정되어 있습니다.", extraCount)
}
