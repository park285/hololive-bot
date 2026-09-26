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
	"net/url"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// CMD_PROFILE 사용자 템플릿의 기존 필드 계약은 유지한다. 소개문 필드는 항상 비어 있다.
type profileTemplateData struct {
	Names       []string
	Catchphrase string
	Summary     string
	Highlights  []string
	DataRows    []profileDataRow
	SocialLinks []profileSocialLink
	OfficialURL string
}
type profileDataRow struct {
	Label     string
	Value     string
	Multiline bool
}
type profileSocialLink struct {
	Label string
	URL   string
}

// FormatMemberInfo는 DB에 등록된 기본 정보만 표시한다. 미등록 날짜·링크는 생략하며 외부 조회를 수행하지 않는다.
func (f *ResponseFormatter) FormatMemberInfo(ctx context.Context, member *domain.Member) string {
	if member == nil {
		return messagestrings.FallbackSentinel
	}

	data := memberInfoTemplateData(member)

	rendered, err := f.render(ctx, domain.TemplateKeyCmdProfile, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return f.foldSeeMore(rendered)
}

func memberInfoTemplateData(member *domain.Member) profileTemplateData {
	data := profileTemplateData{OfficialURL: profileLinkURL(member.OfficialURL)}
	for _, name := range []string{member.NameKo, member.Name, member.NameJa} {
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(data.Names, name) {
			data.Names = append(data.Names, name)
		}
	}

	if member.Org != "" {
		data.DataRows = append(data.DataRows, profileDataRow{Label: "소속", Value: member.Org})
	}

	if len(member.Units) > 0 {
		data.DataRows = append(data.DataRows, profileDataRow{Label: "기수·유닛", Value: strings.Join(member.Units, " / ")})
	}

	status := "활동 중"

	if member.IsGraduated {
		status = "졸업·활동 종료"
	}

	data.DataRows = append(data.DataRows, profileDataRow{Label: "활동 상태", Value: status})

	if member.Birthday != nil {
		data.DataRows = append(data.DataRows, profileDataRow{Label: "생일", Value: member.Birthday.Format("1월 2일")})
	}

	if member.DebutDate != nil {
		data.DataRows = append(data.DataRows, profileDataRow{Label: "데뷔일", Value: member.DebutDate.Format("2006년 1월 2일")})
	}

	if member.ChannelID != "" {
		data.SocialLinks = append(data.SocialLinks, profileSocialLink{Label: "YouTube", URL: "https://www.youtube.com/channel/" + url.PathEscape(member.ChannelID)})
	}

	if member.ChzzkChannelID != "" {
		data.SocialLinks = append(data.SocialLinks, profileSocialLink{Label: "치지직", URL: member.GetChzzkLiveURL()})
	}

	return data
}

func profileLinkURL(raw string) string {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}

	return parsed.String()
}
