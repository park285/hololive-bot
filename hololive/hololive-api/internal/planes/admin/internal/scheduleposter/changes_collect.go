package scheduleposter

import (
	"cmp"
	"context"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const (
	maxChangePages      = 10
	maxAuthorNameRunes  = 60
	maxPostVideos       = 3
	maxStreamTitleRunes = 200
	maxStreamCast       = 8
	quoteHead           = "\n\nQuoted post by "
	entryHead           = "Official schedule entry (this streamer is "
)

type collectedPosts struct {
	posts   []posterstore.Post
	videos  map[int64][]string
	author  string
	seen    map[int64]bool
	newest  time.Time
	reached bool
}

type collectMark struct {
	posterID int64
	newest   time.Time
}

// collectPosts는 페이지 하나라도 실패하면 아무 글도 돌려주지 않는다. 일부만 넣으면 뒤 글이 앞 글보다 먼저 처리된다.
func (s *Service) collectPosts(ctx context.Context, target Target, schedule *posterstore.PublishedSchedule) (collectedPosts, error) {
	floor := later(schedule.PostedAt, weekStartJST(schedule.WeekStart))

	if mark, ok := s.changeLoop.collected[target.ChannelID]; ok && mark.posterID == schedule.PosterID {
		floor = later(floor, mark.newest)
	}

	collected := collectedPosts{seen: map[int64]bool{}, videos: map[int64][]string{}}
	cursor := ""

	for range maxChangePages {
		page, err := s.sources.statuses(ctx, target.XUserID, cursor)
		if err != nil {
			return collectedPosts{}, err
		}

		last := collected.take(&page, target, schedule)

		if len(page.Results) == 0 || page.Cursor.Bottom == "" || !last.IsZero() && !last.After(floor) {
			collected.reached = true

			return collected, nil
		}

		cursor = page.Cursor.Bottom
	}

	return collected, nil
}

// take는 페이지에서 마지막으로 나온 본인 원글의 시각을 돌려준다. 리포스트는 원글의 시각을 달고 오므로 수집 멈춤 판정에 쓰지 않는다.
func (c *collectedPosts) take(page *statusPage, target Target, schedule *posterstore.PublishedSchedule) time.Time {
	weekStart := weekStartJST(schedule.WeekStart)

	var last time.Time

	for i := range page.Results {
		item := &page.Results[i]
		if item.Author.ID != target.XUserID || !item.original() {
			continue
		}

		postedAt := time.Unix(item.CreatedTimestamp, 0)

		last, c.newest = postedAt, later(c.newest, postedAt)

		if !postedAt.After(schedule.PostedAt) || postedAt.Before(weekStart) {
			continue
		}

		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil || id <= 0 || c.seen[id] {
			continue
		}

		body := postBody(item.Text)
		if strings.TrimSpace(body) == "" {
			continue
		}

		texts := []string{item.Text}

		if quote, ok := item.quoted(); ok {
			body = withQuote(body, &quote)
			texts = append(texts, quote.Text)
		}

		c.seen[id] = true
		c.posts = append(c.posts, posterstore.Post{ID: id, PostedAt: postedAt, Body: body})
		c.videos[id] = linkedVideos(texts...)
		c.author = cmp.Or(c.author, displayName(item.Author.Name))
	}

	return last
}

func weekStartJST(weekStart time.Time) time.Time {
	return time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, jst)
}

func postBody(text string) string {
	return cleanPostText(text, posterstore.MaxPostChars)
}

func cleanPostText(text string, limit int) string {
	text = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\r' && unicode.IsControl(r) {
			return ' '
		}

		return r
	}, strings.ToValidUTF8(text, ""))

	if utf8.RuneCountInString(text) <= limit {
		return text
	}

	return string([]rune(text)[:limit])
}

func displayName(name string) string {
	return strings.Join(strings.Fields(cleanPostText(name, maxAuthorNameRunes)), " ")
}

// withQuote는 인용한 글을 본문 뒤에 붙인다. 머리 줄의 문구와 시각 형식은 대응 지시문(match.txt)이 읽는 형식이다.
func withQuote(body string, quote *quotedStatus) string {
	header := quoteHead + displayName(quote.Author.Name) + " at " + time.Unix(quote.CreatedTimestamp, 0).In(jst).Format(matchClockLayout) + " JST:\n"

	room := posterstore.MaxPostChars - utf8.RuneCountInString(body) - utf8.RuneCountInString(header)
	if room <= 0 {
		return body
	}

	return body + header + cleanPostText(quote.Text, room)
}
