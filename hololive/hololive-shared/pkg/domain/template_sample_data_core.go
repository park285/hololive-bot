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

package domain

func templateSampleCoreData() map[TemplateKey]any {
	data := map[TemplateKey]any{}
	addTemplateOutboxSingles(data)
	addTemplateOutboxGroups(data)
	addTemplateCommandStreamSamples(data)
	addTemplateCommandAlarmSamples(data)
	addTemplateAlarmDispatchSamples(data)
	addTemplateDirectoryMilestoneSamples(data)
	addTemplateStatsCalendarSamples(data)
	addTemplateMemberLookupSamples(data)
	addTemplateCelebrationSamples(data)
	return data
}

func addTemplateCelebrationSamples(data map[TemplateKey]any) {
	data[TemplateKeyCelebrationBirthday] = &CelebrationDispatchPayload{
		Kind:       CelebrationKindBirthday,
		MemberName: "시라카미 후부키",
		ChannelID:  "UCdn5BQ06XqgXoAxIhbqw5Rg",
		Ordinal:    2,
	}
	data[TemplateKeyCelebrationAnniversary] = &CelebrationDispatchPayload{
		Kind:       CelebrationKindAnniversary,
		MemberName: "토키노 소라",
		ChannelID:  "UCp6993wxpyDPHUpavwDFqgg",
		Years:      7,
	}
	data[TemplateKeyCelebrationBirthdayStream] = &CelebrationDispatchPayload{
		Kind:              CelebrationKindBirthdayStream,
		MemberName:        "시라카미 후부키",
		ChannelID:         "UCdn5BQ06XqgXoAxIhbqw5Rg",
		VideoID:           "birthday-stream-1",
		StreamTitle:       "【생일 방송】후부키 생일 기념 라이브!",
		StreamURL:         "https://www.youtube.com/watch?v=birthday-stream-1",
		ScheduledStartKST: "21:00",
	}
}

func addTemplateMemberLookupSamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdMemberNotLive] = map[string]any{
		fieldMemberName: sampleMemberMiko,
	}
	data[TemplateKeyCmdMemberNoUpcoming] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		"Hours":         24,
	}
	data[TemplateKeyCmdMemberNotFound] = map[string]any{
		fieldMemberName: sampleMemberMiko,
	}
	data[TemplateKeyCmdAmbiguousMember] = map[string]any{
		fieldPrefix:      "!",
		"CommandExample": "알람 추가",
		"FirstName":      "사쿠라 미코 (Hololive)",
		"Candidates": []map[string]any{
			{"Index": 1, fieldName: "사쿠라 미코 (Hololive)"},
			{"Index": 2, fieldName: "사쿠라 미코 (Nijisanji)"},
		},
	}
}

func addTemplateStatsCalendarSamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdStatsCount] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		"Subscribers":   sampleSubs200Man,
	}
	data[TemplateKeyCmdStatsGainers] = map[string]any{
		"Period": "주간",
		"Gainers": []map[string]any{
			{"Rank": 1, fieldMemberName: sampleMemberMiko, "Delta": "1만 2345", "Current": sampleSubs200Man},
			{"Rank": 2, fieldMemberName: "호시마치 스이세이", "Delta": "8500", "Current": "205만"},
		},
	}
	data[TemplateKeyCmdCalendar] = map[string]any{
		"Year":     2026,
		"Month":    6,
		fieldCount: 3,
		"Days": []map[string]any{
			{"Month": 6, "Day": 10, "Entries": []map[string]any{
				{fieldName: "유키하나 라미", "IsBirthday": true, "Years": 0},
				{fieldName: "유키하나 라미", "IsBirthday": false, "Years": 2},
			}},
			{"Month": 6, "Day": 20, "Entries": []map[string]any{
				{fieldName: "시시로 보탄", "IsBirthday": true, "Years": 0},
			}},
		},
	}
}

func addTemplateOutboxSingles(data map[TemplateKey]any) {
	data[TemplateKeyOutboxShorts] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "NEW_SHORT",
		fieldTitle:      "새 쇼츠 제목 - 귀여운 미코치",
		fieldURL:        "https://www.youtube.com/shorts/abc123xyz",
		"VideoID":       "abc123xyz",
	}
	data[TemplateKeyOutboxCommunity] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "COMMUNITY_POST",
		"ContentText":   "오늘 밤 10시에 방송합니다! 많이 놀러오세요~",
		fieldURL:        "https://www.youtube.com/post/Ugkxyz123",
		"PostID":        "Ugkxyz123",
	}
	data[TemplateKeyOutboxVideo] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "NEW_VIDEO",
		fieldTitle:      "마인크래프트 건축 배틀 #미코라이브",
		fieldURL:        "https://youtu.be/video123xyz",
		"VideoID":       "video123xyz",
	}
	data[TemplateKeyOutboxMilestone] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "MILESTONE",
		"Milestone":     sampleSubs200Man,
	}
}

func addTemplateOutboxGroups(data map[TemplateKey]any) {
	data[TemplateKeyOutboxVideoGroup] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "NEW_VIDEO",
		fieldCount:      2,
		"Items":         templateOutboxVideoGroupItems(),
	}
	data[TemplateKeyOutboxShortsGroup] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "NEW_SHORT",
		fieldCount:      2,
		"Items":         templateOutboxShortsGroupItems(),
	}
	data[TemplateKeyOutboxCommunityGroup] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		fieldKind:       "COMMUNITY_POST",
		fieldCount:      2,
		"Items":         templateOutboxCommunityGroupItems(),
	}
}

func templateOutboxVideoGroupItems() []map[string]any {
	return []map[string]any{
		{fieldTitle: "마인크래프트 건축 배틀 #1", fieldURL: "https://youtu.be/group-video-1"},
		{fieldTitle: "마인크래프트 건축 배틀 #2", fieldURL: "https://youtu.be/group-video-2"},
	}
}

func templateOutboxShortsGroupItems() []map[string]any {
	return []map[string]any{
		{fieldTitle: "오늘의 쇼츠 #1", fieldURL: "https://www.youtube.com/shorts/group-1"},
		{fieldTitle: "오늘의 쇼츠 #2", fieldURL: "https://www.youtube.com/shorts/group-2"},
	}
}

func templateOutboxCommunityGroupItems() []map[string]any {
	return []map[string]any{
		{"ContentText": "오늘 밤 10시 방송 공지", fieldURL: "https://www.youtube.com/post/group-community-1"},
		{"ContentText": "굿즈 판매 시작 안내", fieldURL: "https://www.youtube.com/post/group-community-2"},
	}
}

func addTemplateCommandStreamSamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdHelp] = map[string]any{
		fieldEmoji:  map[string]string{"Mic": "🎙", "Star": "⭐", fieldBell: "🔔", "Clock": "⏰", "Sparkle": "✨"},
		fieldPrefix: "!",
	}
	data[TemplateKeyCmdLiveStreams] = map[string]any{
		fieldEmoji: map[string]string{"Live": "🔴"},
		fieldCount: 3,
		"Streams":  templateLiveStreamSamples(),
	}
	data[TemplateKeyCmdUpcomingStreams] = map[string]any{
		fieldEmoji: map[string]string{"Calendar": "📅"},
		fieldCount: 2,
		"Hours":    24,
		"Streams":  templateUpcomingStreamSamples(),
	}
	data[TemplateKeyCmdChannelSchedule] = map[string]any{
		fieldEmoji:       map[string]string{"Calendar": "📅"},
		fieldChannelName: sampleMemberMiko,
		"Days":           7,
		fieldCount:       5,
		"Streams":        templateChannelScheduleSamples(),
	}
}

func templateLiveStreamSamples() []map[string]any {
	return []map[string]any{
		{fieldChannelName: sampleMemberMiko, fieldTitle: "마인크래프트 건축 배틀", fieldURL: "https://youtu.be/live123", "ViewerCount": 15000},
		{fieldChannelName: "오오조라 스바루", fieldTitle: "잡담 방송", fieldURL: "https://youtu.be/live456", "ViewerCount": 8500},
	}
}

func templateUpcomingStreamSamples() []map[string]any {
	return []map[string]any{
		{fieldChannelName: "시라카미 후부키", fieldTitle: "노래방 방송", "TimeInfo": "30분 후", fieldURL: "https://youtu.be/upcoming123"},
	}
}

func templateChannelScheduleSamples() []map[string]any {
	return []map[string]any{
		{"IsLive": true, fieldTitle: "마인크래프트", "TimeInfo": "방송 중", fieldURL: "https://youtu.be/live123"},
		{"IsLive": false, fieldTitle: "게임 방송", "TimeInfo": "오늘 22:00", fieldURL: "https://youtu.be/upcoming456"},
	}
}

func addTemplateCommandAlarmSamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdAlarmList] = map[string]any{
		fieldEmoji:  map[string]string{fieldBell: "🔔"},
		fieldCount:  3,
		fieldPrefix: "!",
		"Alarms":    []map[string]any{templateAlarmListItem()},
	}
	data[TemplateKeyCmdAlarmNotification] = templateAlarmNotificationSample(5)
	data[TemplateKeyCmdAlarmLiveStarted] = templateAlarmNotificationSample(0)
	data[TemplateKeyCmdAlarmNotificationGroup] = templateAlarmNotificationGroupSample()
	data[TemplateKeyCmdAlarmAdded] = map[string]any{
		fieldEmoji:      map[string]string{fieldBell: "🔔", "Check": "✅"},
		fieldMemberName: sampleMemberMiko,
		"Added":         true,
		fieldPrefix:     "!",
		"NextStream":    templateNextStreamSample(),
	}
	data[TemplateKeyCmdAlarmRemoved] = map[string]any{
		fieldEmoji:      map[string]string{fieldBell: "🔕"},
		fieldMemberName: sampleMemberMiko,
		"Removed":       true,
	}
	data[TemplateKeyCmdAlarmCleared] = map[string]any{
		fieldEmoji: map[string]string{fieldBell: "🔕"},
		fieldCount: 5,
	}
}

func addTemplateAlarmDispatchSamples(data map[TemplateKey]any) {
	data[TemplateKeyAlarmDispatchNotification] = map[string]any{
		"IsStarting":      false,
		"IsScheduled":     false,
		fieldMemberName:   sampleMemberMiko,
		"MinutesUntil":    5,
		fieldTitle:        "마인크래프트 건축",
		"ScheduleMessage": "",
		fieldURL:          "https://youtu.be/stream123",
	}
	data[TemplateKeyAlarmDispatchNotificationGroup] = map[string]any{
		"IsStarting":   false,
		"MinutesUntil": 5,
		"Entries": []map[string]any{
			{"IsStarting": false, "IsScheduled": false, fieldMemberName: sampleMemberMiko, "MinutesUntil": 5, fieldTitle: "마인크래프트 건축", "ScheduleMessage": "", fieldURL: "https://youtu.be/stream123"},
			{"IsStarting": false, "IsScheduled": true, fieldMemberName: "호시마치 스이세이", "MinutesUntil": 0, fieldTitle: "노래 방송", "ScheduleMessage": "21:00 시작 예정", fieldURL: ""},
		},
	}
}

func templateAlarmListItem() map[string]any {
	return map[string]any{
		fieldMemberName: sampleMemberMiko,
		"TypesLabel":    "라이브, 쇼츠",
		"NextStream":    templateNextStreamSample(),
	}
}

func templateNextStreamSample() map[string]any {
	return map[string]any{
		"Status":       "예정",
		fieldTitle:     "마인크래프트",
		fieldURL:       "https://youtu.be/upcoming123",
		"ScheduledKST": "22:00",
		"TimeDetail":   "2시간 후",
		"StartingSoon": false,
	}
}

func templateAlarmNotificationGroupSample() map[string]any {
	return map[string]any{
		fieldCount:       2,
		"MinutesUntil":   5,
		"ScheduledTimes": []string{"21:00"},
		"Entries": []map[string]any{
			{"Index": 1, fieldChannelName: sampleMemberMiko, "ScheduledKST": "21:00", fieldTitle: "마인크래프트 건축", fieldURL: "https://youtu.be/stream123"},
			{"Index": 2, fieldChannelName: "호시마치 스이세이", "ScheduledKST": "", fieldTitle: "노래 방송", fieldURL: "https://youtu.be/stream456"},
		},
	}
}

func templateAlarmNotificationSample(minutesUntil int) map[string]any {
	return map[string]any{
		fieldEmoji:         map[string]string{fieldBell: "🔔"},
		fieldChannelName:   sampleMemberMiko,
		fieldTitle:         "마인크래프트 건축",
		"MinutesUntil":     minutesUntil,
		fieldURL:           "https://youtu.be/stream123",
		"ScheduledTimeKST": "21:00",
		"ScheduleMessage":  "",
	}
}

func addTemplateDirectoryMilestoneSamples(data map[TemplateKey]any) {
	data[TemplateKeyCmdMemberDirectory] = map[string]any{
		fieldEmoji: map[string]string{"Star": "⭐"},
		"Total":    50,
		"Groups":   []map[string]any{templateMemberDirectoryGroup()},
	}
	data[TemplateKeyCmdProfile] = templateProfileSample()
	data[TemplateKeyCmdMilestoneAchieved] = map[string]any{
		fieldMemberName: sampleMemberMiko,
		"Milestone":     sampleSubs200Man,
		fieldEmoji:      map[string]string{"Trophy": "🏆"},
	}
	data[TemplateKeyCmdMilestoneApproach] = map[string]any{
		fieldMemberName:   sampleMemberMiko,
		"CurrentSubs":     1990000,
		"Milestone":       sampleSubs200Man,
		"TargetMilestone": sampleSubs200Man,
		"Remaining":       10000,
		fieldEmoji:        map[string]string{"Rocket": "🚀"},
	}
}

func templateProfileSample() map[string]any {
	return map[string]any{
		"Names":       []string{"시라카미 후부키", "Shirakami Fubuki", "白上フブキ"},
		"Catchphrase": "친구야!",
		"Summary":     "홀로라이브 1기생 여우 VTuber",
		"Highlights":  []string{"고양이 아님", "FOX"},
		"DataRows": []map[string]any{
			{"Label": "생일", "Value": "10월 5일", "Multiline": false},
			{"Label": "특기", "Value": "  노래\n  게임", "Multiline": true},
		},
		"SocialLinks": []map[string]any{
			{"Label": "음악 플레이리스트", fieldURL: "https://www.youtube.com/playlist?list=example"},
			{"Label": "Twitter", fieldURL: "https://x.com/shirakamifubuki"},
		},
		"OfficialURL": "https://hololive.hololivepro.com/talents/shirakami-fubuki",
	}
}

func templateMemberDirectoryGroup() map[string]any {
	return map[string]any{
		"GroupName": "JP 0기생",
		"Members": []map[string]any{
			{"Primary": "토키노 소라", "Secondary": "Tokino Sora", "ShowBoth": true},
			{"Primary": "AZKi", "Secondary": "", "ShowBoth": false},
		},
	}
}
