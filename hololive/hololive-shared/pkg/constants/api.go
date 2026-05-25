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

var HolodexAPIParams = struct {
	OrgHololive         string
	OrgVSpo             string
	OrgStellive         string
	OrgIndie            string
	OrgAll              string
	StatusLive          string
	StatusUpcoming      string
	TypeStream          string
	TypeVtuber          string
	MaxUpcomingHours    int
	DefaultChannelLimit int
	MaxPaginationOffset int
	SyncTargetOrgs      []string
	AllowedFilterOrgs   []string
}{
	OrgHololive:         "Hololive",
	OrgVSpo:             "VSpo",
	OrgStellive:         "Stellive",
	OrgIndie:            "Independents",
	OrgAll:              "all",
	StatusLive:          "live",
	StatusUpcoming:      "upcoming",
	TypeStream:          "stream",
	TypeVtuber:          "vtuber",
	MaxUpcomingHours:    168,
	DefaultChannelLimit: 50,
	MaxPaginationOffset: 500,
	SyncTargetOrgs:      []string{"Hololive", "VSpo", "Stellive"},
	AllowedFilterOrgs:   []string{"Hololive", "VSpo", "Independents", "Stellive"},
}

var IndieChannelIDs = []string{
	"UCrV1Hf5r8P148idjoSfrGEQ", // 結城さくな (Yuuki Sakuna)
	"UCxsZ6NCzjU_t4YSxQLBcM5A", // 사메코 사바 (Sameko Saba)
}
