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

package constants

import "time"

var StringLimits = struct {
	StreamTitle int
}{
	StreamTitle: 100,
}

var ValkeyTimeout = struct {
	ConnWriteTimeout time.Duration
	DialTimeout      time.Duration
}{
	ConnWriteTimeout: 3 * time.Second,
	DialTimeout:      5 * time.Second,
}

var APIRateLimitConfig = struct {
	Enabled bool
	Limit   int
	Window  time.Duration
}{
	Enabled: true,
	Limit:   60, // 분당 60회
	Window:  time.Minute,
}

var MajorEventConfig = struct {
	TrustedSourceDomains   []string
	TrustedSocialAccounts  []string
	SearchSourceSites      []string
	SearchOfficialAccounts []string
	SearchPartnerKeywords  []string
	ScheduleHourKST        int
	ScheduleWeekday        time.Weekday
	MonthlyScheduleHourKST int
	MonthlyScheduleDay     int
}{
	TrustedSourceDomains: []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"cover-corp.com",
		"hololive.tv",
		"schedule.hololive.tv",
		"shop.hololivepro.com",
		"hololive-official-cardgame.com",
		"aniplustv.com",
		"aniplus.co.kr",
		"animate.co.jp",
		"lawson.co.jp",
	},
	TrustedSocialAccounts: []string{
		"hololivetv",
		"hololive_en",
		"hololive_id",
		"holostarsen",
		"hololive_ocg_en",
		"aniplus_shop",
		"v_square_kr",
		"agf_korea",
	},
	SearchSourceSites: []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"x.com",
		"twitter.com",
		"schedule.hololive.tv",
		"shop.hololivepro.com",
		"hololive-official-cardgame.com",
		"aniplustv.com",
		"aniplus.co.kr",
	},
	SearchOfficialAccounts: []string{
		"hololivetv",
		"hololive_en",
		"hololive_id",
		"HOLOSTARSen",
		"hololive_OCG_EN",
		"ANIPLUS_SHOP",
		"v_square_kr",
	},
	SearchPartnerKeywords: []string{
		"ANIPLUS",
		"V-SQUARE",
		"AGF Korea",
		"collaboration cafe",
	},
	ScheduleHourKST:        9,
	ScheduleWeekday:        time.Monday,
	MonthlyScheduleHourKST: 10,
	MonthlyScheduleDay:     1,
}
