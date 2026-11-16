package scheduleposter

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
)

var videoLinkPattern = regexp.MustCompile(`(?:youtube\.com/(?:live/|watch\?(?:[^\s#]*&)?v=)|youtu\.be/)([A-Za-z0-9_-]{11})(?:[^A-Za-z0-9_-]|$)`)

func linkedVideos(texts ...string) []string {
	var ids []string

	for _, text := range texts {
		for _, match := range videoLinkPattern.FindAllStringSubmatch(text, -1) {
			if len(ids) < maxPostVideos && !slices.Contains(ids, match[1]) {
				ids = append(ids, match[1])
			}
		}
	}

	return ids
}

type streamCast struct {
	index officialidentity.Index
	names map[string]string
}

func newStreamCast(members []*domain.Member) streamCast {
	cast := streamCast{index: officialidentity.New(members), names: make(map[string]string, len(members))}

	for _, member := range members {
		if member != nil && member.ChannelID != "" {
			cast.names[member.ChannelID] = cmp.Or(member.NameJa, member.Name)
		}
	}

	return cast
}

func (c streamCast) joins(stream *posterstore.OfficialStream, channelID string) bool {
	return stream.ChannelID == channelID ||
		slices.ContainsFunc(stream.Guests, func(name string) bool { return c.index.Resolve(name) == channelID })
}

// streamEntry의 머리 줄과 줄 이름은 대응 지시문(match.txt)이 읽는 형식이다.
func (c streamCast) streamEntry(stream *posterstore.OfficialStream, channelID string) string {
	hosts := stream.ChannelID == channelID
	host := c.names[stream.ChannelID]

	var entry strings.Builder

	entry.WriteString(entryHead + map[bool]string{true: "the host", false: "a guest"}[hosts] + "):")
	entry.WriteString("\nStart: " + stream.StartsAt.In(jst).Format(matchClockLayout) + " JST")
	entry.WriteString("\nTitle: " + oneLine(stream.Title, maxStreamTitleRunes))

	with := make([]string, 0, len(stream.Guests)+1)

	if !hosts && host != "" {
		entry.WriteString("\nChannel: " + host)

		with = append(with, host)
	}

	for _, name := range stream.Guests {
		guest := c.index.Resolve(name)
		if name = oneLine(name, maxAuthorNameRunes); name != "" && guest != channelID && (guest == "" || guest != stream.ChannelID) {
			with = append(with, name)
		}
	}

	if rest := len(with) - maxStreamCast; rest > 0 {
		with = append(with[:maxStreamCast:maxStreamCast], "and "+strconv.Itoa(rest)+" others")
	}

	if len(with) > 0 {
		entry.WriteString("\nWith: " + strings.Join(with, ", "))
	}

	return entry.String()
}

func oneLine(text string, limit int) string {
	return strings.Join(strings.Fields(cleanPostText(text, limit)), " ")
}

func (s *Service) attachStreams(
	ctx context.Context, target Target, collected *collectedPosts, roster *changeRoster,
) (map[string][]posterstore.Post, error) {
	mentions := map[string][]posterstore.Post{}

	streams, err := s.linkedStreams(ctx, collected)
	if err != nil {
		return nil, err
	}

	if len(streams) == 0 {
		return mentions, nil
	}

	members, err := roster.load(ctx, s.members)
	if err != nil {
		return nil, fmt.Errorf("load members for official streams: %w", err)
	}

	cast := newStreamCast(members)

	for i := range collected.posts {
		post := &collected.posts[i]

		for channelID, text := range s.streamEntries(post, collected.videos[post.ID], streams, target, cast) {
			switch {
			case channelID != target.ChannelID:
				body := "Post by " + cmp.Or(collected.author, "another streamer") + " about a stream this streamer joins.\n\n" + text

				mentions[channelID] = append(mentions[channelID], posterstore.Post{
					ID: post.ID, PostedAt: post.PostedAt, Body: cleanPostText(body, posterstore.MaxPostChars),
				})
			case utf8.RuneCountInString(post.Body)+2+utf8.RuneCountInString(text) <= posterstore.MaxPostChars:
				post.Body += "\n\n" + text
			}
		}
	}

	return mentions, nil
}

func (s *Service) linkedStreams(ctx context.Context, collected *collectedPosts) (map[string]posterstore.OfficialStream, error) {
	var linked []string

	for _, ids := range collected.videos {
		linked = append(linked, ids...)
	}

	if len(linked) == 0 {
		return map[string]posterstore.OfficialStream{}, nil
	}

	slices.Sort(linked)

	streams, err := s.ledger.OfficialStreams(ctx, slices.Compact(linked))
	if err != nil {
		return nil, fmt.Errorf("load official streams for schedule poster posts: %w", err)
	}

	return streams, nil
}

func (s *Service) streamEntries(
	post *posterstore.Post, videos []string, streams map[string]posterstore.OfficialStream, target Target, cast streamCast,
) map[string]string {
	entries := map[string]string{}

	for _, id := range videos {
		stream, ok := streams[id]
		if !ok || !stream.StartsAt.After(post.PostedAt) || !cast.joins(&stream, target.ChannelID) {
			continue
		}

		for _, channelID := range s.streamAudience(&stream, target, cast) {
			if entries[channelID] != "" {
				entries[channelID] += "\n\n"
			}

			entries[channelID] += cast.streamEntry(&stream, channelID)
		}
	}

	return entries
}

// streamAudience는 방송을 여는 멤버를 다른 사람 글의 대상에서 뺀다. 그 멤버의 일정은 본인 안내 글이 반영한다.
func (s *Service) streamAudience(stream *posterstore.OfficialStream, target Target, cast streamCast) []string {
	audience := []string{target.ChannelID}

	for _, name := range stream.Guests {
		channelID := cast.index.Resolve(name)
		if channelID == "" || channelID == stream.ChannelID || slices.Contains(audience, channelID) ||
			!slices.ContainsFunc(s.config.Targets, func(other Target) bool { return other.ChannelID == channelID }) {
			continue
		}

		audience = append(audience, channelID)
	}

	return audience
}

func (c *changeLoopState) keepMentions(mentions map[string][]posterstore.Post) {
	for channelID, posts := range mentions {
		if c.mentions[channelID] == nil {
			c.mentions[channelID] = map[int64]posterstore.Post{}
		}

		for i := range posts {
			c.mentions[channelID][posts[i].ID] = posts[i]
		}
	}
}

func (c *changeLoopState) dropMentionsBefore(cutoff time.Time) {
	for channelID, posts := range c.mentions {
		maps.DeleteFunc(posts, func(_ int64, post posterstore.Post) bool { return post.PostedAt.Before(cutoff) })

		if len(posts) == 0 {
			delete(c.mentions, channelID)
		}
	}
}

func (c *changeLoopState) mentionsFor(channelID string) []posterstore.Post {
	return slices.SortedFunc(maps.Values(c.mentions[channelID]), func(a, b posterstore.Post) int {
		return cmp.Or(a.PostedAt.Compare(b.PostedAt), cmp.Compare(a.ID, b.ID))
	})
}
