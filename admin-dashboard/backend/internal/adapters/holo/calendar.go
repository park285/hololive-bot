package holo

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kapu/admin-dashboard/internal/contract"
)

// CalendarMember는 달력에 필요한 식별자와 표시 필드만 보존합니다.
type CalendarMember struct {
	ID              string  `json:"id"`
	ChannelID       string  `json:"channelId"`
	Name            string  `json:"name"`
	NameKO          *string `json:"nameKo,omitempty"`
	ShortKoreanName *string `json:"shortKoreanName,omitempty"`
	Photo           *string `json:"photo,omitempty"`
	Org             *string `json:"org,omitempty"`
	Suborg          *string `json:"suborg,omitempty"`
	IsGraduated     *bool   `json:"isGraduated,omitempty"`
}

// CalendarEntry는 upstream이 확인한 기념일과 표시 순서를 유지합니다.
type CalendarEntry struct {
	Kind    string         `json:"kind"`
	Member  CalendarMember `json:"member"`
	Day     int            `json:"day"`
	Ordinal *int           `json:"ordinal,omitempty"`
}

// CalendarResponse는 조회한 월·연도와 기념일 목록입니다.
type CalendarResponse struct {
	Status  string          `json:"status"`
	Month   int             `json:"month"`
	Year    int             `json:"year"`
	Entries []CalendarEntry `json:"entries"`
}

// CalendarQuery의 nil은 upstream의 현재 한국 시각 기본값을 사용한다는 뜻입니다.
type CalendarQuery struct {
	Month *int
	Year  *int
}

type upstreamCalendarMember struct {
	upstreamMember

	ShortKoreanName *string `json:"shortKoreanName"`
	Photo           *string `json:"photo"`
	Org             *string `json:"org"`
	Suborg          *string `json:"suborg"`
}

type upstreamCalendarEntry struct {
	Kind    *string                 `json:"kind"`
	Member  *upstreamCalendarMember `json:"member"`
	Day     *int                    `json:"day"`
	Ordinal *int                    `json:"ordinal"`
}

type upstreamCalendarResponse struct {
	Status  string                  `json:"status"`
	Month   *int                    `json:"month"`
	Year    *int                    `json:"year"`
	Entries []upstreamCalendarEntry `json:"entries"`
}

func (query CalendarQuery) values() (url.Values, error) {
	values := make(url.Values)

	if query.Month != nil {
		if *query.Month < 1 || *query.Month > 12 {
			return nil, contract.BadRequest("month must be between 1 and 12")
		}

		values.Set("month", strconv.Itoa(*query.Month))
	}

	if query.Year != nil {
		if *query.Year < 2000 || *query.Year > 2100 {
			return nil, contract.BadRequest("year must be between 2000 and 2100")
		}

		values.Set("year", strconv.Itoa(*query.Year))
	}

	return values, nil
}

// GetCalendar는 upstream의 1~12월·2000~2100년 범위를 검증한 뒤 고정 경로를 조회합니다.
func (c *Client) GetCalendar(ctx context.Context, query CalendarQuery) (CalendarResponse, error) {
	values, err := query.values()
	if err != nil {
		return CalendarResponse{}, err
	}

	var response upstreamCalendarResponse

	if err := c.request(ctx, http.MethodGet, "/api/holo/members/calendar", values, nil, http.StatusOK, &response); err != nil {
		return CalendarResponse{}, err
	}

	return projectCalendarResponse(response, query)
}

func projectCalendarResponse(wire upstreamCalendarResponse, query CalendarQuery) (CalendarResponse, error) {
	if wire.Status != "ok" || wire.Month == nil || wire.Year == nil || wire.Entries == nil {
		return CalendarResponse{}, invalidResponse()
	}

	if *wire.Month < 1 || *wire.Month > 12 || *wire.Year < 2000 || *wire.Year > 2100 {
		return CalendarResponse{}, invalidResponse()
	}

	if query.Month != nil && *query.Month != *wire.Month || query.Year != nil && *query.Year != *wire.Year {
		return CalendarResponse{}, invalidResponse()
	}

	out := CalendarResponse{Status: "ok", Month: *wire.Month, Year: *wire.Year, Entries: make([]CalendarEntry, 0, len(wire.Entries))}
	for _, entry := range wire.Entries {
		mapped, err := projectCalendarEntry(entry)
		if err != nil {
			return CalendarResponse{}, err
		}

		out.Entries = append(out.Entries, mapped)
	}

	return out, nil
}

func projectCalendarEntry(entry upstreamCalendarEntry) (CalendarEntry, error) {
	member := entry.Member
	if entry.Kind == nil || entry.Day == nil || member == nil || member.ID <= 0 || member.ChannelID == nil || member.Name == nil {
		return CalendarEntry{}, invalidResponse()
	}

	return CalendarEntry{Kind: *entry.Kind, Day: *entry.Day, Ordinal: entry.Ordinal, Member: CalendarMember{
		ID: strconv.FormatInt(member.ID, 10), ChannelID: *member.ChannelID, Name: *member.Name,
		NameKO: member.NameKO, ShortKoreanName: member.ShortKoreanName, Photo: member.Photo, Org: member.Org, Suborg: member.Suborg, IsGraduated: member.IsGraduated,
	}}, nil
}
