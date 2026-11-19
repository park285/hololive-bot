package formatter

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

func (f *ResponseFormatter) FormatMemberInfo(ctx context.Context, member *domain.Member) string {
	if member == nil {
		return f.renderFailureText()
	}

	data := memberInfoTemplateData(member)

	return f.renderResponse(ctx, domain.TemplateKeyCmdProfile, data, true)
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

	return data
}

func profileLinkURL(raw string) string {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}

	return parsed.String()
}
