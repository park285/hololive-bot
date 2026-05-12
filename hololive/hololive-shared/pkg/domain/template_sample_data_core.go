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
	return map[TemplateKey]any{
		TemplateKeyOutboxShorts: map[string]any{
			"MemberName": "사쿠라 미코",
			"Title":      "새 쇼츠 제목 - 귀여운 미코치",
			"URL":        "https://www.youtube.com/shorts/abc123xyz",
			"VideoID":    "abc123xyz",
		},
		TemplateKeyOutboxCommunity: map[string]any{
			"MemberName":  "사쿠라 미코",
			"ContentText": "오늘 밤 10시에 방송합니다! 많이 놀러오세요~",
			"URL":         "https://www.youtube.com/post/Ugkxyz123",
			"PostID":      "Ugkxyz123",
		},
		TemplateKeyOutboxVideo: map[string]any{
			"MemberName": "사쿠라 미코",
			"Title":      "마인크래프트 건축 배틀 #미코라이브",
			"URL":        "https://youtu.be/video123xyz",
			"VideoID":    "video123xyz",
		},
		TemplateKeyOutboxMilestone: map[string]any{
			"MemberName": "사쿠라 미코",
			"Milestone":  "200만",
		},
		TemplateKeyOutboxVideoGroup: map[string]any{
			"MemberName": "사쿠라 미코",
			"Count":      2,
			"Items": []map[string]any{
				{
					"Title": "마인크래프트 건축 배틀 #1",
					"URL":   "https://youtu.be/group-video-1",
				},
				{
					"Title": "마인크래프트 건축 배틀 #2",
					"URL":   "https://youtu.be/group-video-2",
				},
			},
		},
		TemplateKeyOutboxShortsGroup: map[string]any{
			"MemberName": "사쿠라 미코",
			"Count":      2,
			"Items": []map[string]any{
				{
					"Title": "오늘의 쇼츠 #1",
					"URL":   "https://www.youtube.com/shorts/group-1",
				},
				{
					"Title": "오늘의 쇼츠 #2",
					"URL":   "https://www.youtube.com/shorts/group-2",
				},
			},
		},
		TemplateKeyOutboxCommunityGroup: map[string]any{
			"MemberName": "사쿠라 미코",
			"Count":      2,
			"Items": []map[string]any{
				{
					"ContentText": "오늘 밤 10시 방송 공지",
					"URL":         "https://www.youtube.com/post/group-community-1",
				},
				{
					"ContentText": "굿즈 판매 시작 안내",
					"URL":         "https://www.youtube.com/post/group-community-2",
				},
			},
		},

		TemplateKeyCmdHelp: map[string]any{
			"Emoji": map[string]string{
				"Mic":     "🎙",
				"Star":    "⭐",
				"Bell":    "🔔",
				"Clock":   "⏰",
				"Sparkle": "✨",
			},
			"Prefix": "!",
		},

		TemplateKeyCmdLiveStreams: map[string]any{
			"Emoji": map[string]string{
				"Live": "🔴",
			},
			"Count": 3,
			"Streams": []map[string]any{
				{
					"ChannelName": "사쿠라 미코",
					"Title":       "마인크래프트 건축 배틀",
					"URL":         "https://youtu.be/live123",
					"ViewerCount": 15000,
				},
				{
					"ChannelName": "오오조라 스바루",
					"Title":       "잡담 방송",
					"URL":         "https://youtu.be/live456",
					"ViewerCount": 8500,
				},
			},
		},

		TemplateKeyCmdUpcomingStreams: map[string]any{
			"Emoji": map[string]string{
				"Calendar": "📅",
			},
			"Count": 2,
			"Hours": 24,
			"Streams": []map[string]any{
				{
					"ChannelName": "시라카미 후부키",
					"Title":       "노래방 방송",
					"TimeInfo":    "30분 후",
					"URL":         "https://youtu.be/upcoming123",
				},
			},
		},

		TemplateKeyCmdChannelSchedule: map[string]any{
			"Emoji": map[string]string{
				"Calendar": "📅",
			},
			"ChannelName": "사쿠라 미코",
			"Days":        7,
			"Count":       5,
			"Streams": []map[string]any{
				{
					"IsLive":   true,
					"Title":    "마인크래프트",
					"TimeInfo": "방송 중",
					"URL":      "https://youtu.be/live123",
				},
				{
					"IsLive":   false,
					"Title":    "게임 방송",
					"TimeInfo": "오늘 22:00",
					"URL":      "https://youtu.be/upcoming456",
				},
			},
		},

		TemplateKeyCmdAlarmList: map[string]any{
			"Emoji": map[string]string{
				"Bell": "🔔",
			},
			"Count":  3,
			"Prefix": "!",
			"Alarms": []map[string]any{
				{
					"MemberName": "사쿠라 미코",
					"TypesLabel": "라이브, 쇼츠",
					"NextStream": map[string]any{
						"Status":       "예정",
						"Title":        "마인크래프트",
						"URL":          "https://youtu.be/upcoming123",
						"ScheduledKST": "22:00",
						"TimeDetail":   "2시간 후",
						"StartingSoon": false,
					},
				},
			},
		},

		TemplateKeyCmdAlarmNotification: map[string]any{
			"Emoji": map[string]string{
				"Bell": "🔔",
			},
			"ChannelName":      "사쿠라 미코",
			"Title":            "마인크래프트 건축",
			"MinutesUntil":     5,
			"URL":              "https://youtu.be/stream123",
			"ScheduledTimeKST": "21:00",
			"ScheduleMessage":  "",
		},
		TemplateKeyCmdAlarmLiveStarted: map[string]any{
			"Emoji": map[string]string{
				"Bell": "🔔",
			},
			"ChannelName":      "사쿠라 미코",
			"Title":            "마인크래프트 건축",
			"MinutesUntil":     0,
			"URL":              "https://youtu.be/stream123",
			"ScheduledTimeKST": "21:00",
			"ScheduleMessage":  "",
		},

		TemplateKeyCmdAlarmAdded: map[string]any{
			"Emoji": map[string]string{
				"Bell":  "🔔",
				"Check": "✅",
			},
			"MemberName": "사쿠라 미코",
			"Added":      true,
			"Prefix":     "!",
			"NextStream": map[string]any{
				"Status":       "예정",
				"Title":        "마인크래프트",
				"URL":          "https://youtu.be/upcoming123",
				"ScheduledKST": "22:00",
				"TimeDetail":   "2시간 후",
				"StartingSoon": false,
			},
		},

		TemplateKeyCmdAlarmRemoved: map[string]any{
			"Emoji": map[string]string{
				"Bell": "🔕",
			},
			"MemberName": "사쿠라 미코",
			"Removed":    true,
		},

		TemplateKeyCmdAlarmCleared: map[string]any{
			"Emoji": map[string]string{
				"Bell": "🔕",
			},
			"Count": 5,
		},

		TemplateKeyCmdMemberDirectory: map[string]any{
			"Emoji": map[string]string{
				"Star": "⭐",
			},
			"Total": 50,
			"Groups": []map[string]any{
				{
					"GroupName": "JP 0기생",
					"Members": []map[string]any{
						{"Primary": "토키노 소라", "Secondary": "Tokino Sora", "ShowBoth": true},
						{"Primary": "AZKi", "Secondary": "", "ShowBoth": false},
					},
				},
			},
		},

		TemplateKeyCmdMilestoneAchieved: map[string]any{
			"MemberName": "사쿠라 미코",
			"Milestone":  "200만",
			"Emoji": map[string]string{
				"Trophy": "🏆",
			},
		},

		TemplateKeyCmdMilestoneApproach: map[string]any{
			"MemberName":      "사쿠라 미코",
			"CurrentSubs":     1990000,
			"TargetMilestone": "200만",
			"Remaining":       10000,
			"Emoji": map[string]string{
				"Rocket": "🚀",
			},
		},
	}
}
