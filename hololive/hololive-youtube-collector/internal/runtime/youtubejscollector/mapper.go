package youtubejscollector

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

func communityPayload(channelID string, posts []*parser.CommunityPost, maxResults int, page *youtubejs.Pagination) contract.CommunityPayloadV1 {
	mapped := make([]contract.CommunityPostV1, 0, len(posts))
	for _, post := range posts {
		mapped = append(mapped, contract.CommunityPostV1{
			PostID:         post.PostID,
			UpstreamPostID: post.UpstreamPostID,
			ChannelID:      channelID,
			AuthorID:       post.AuthorID,
			AuthorName:     post.AuthorName,
			AuthorPhoto:    thumbnails(post.AuthorPhoto),
			ContentText:    post.ContentText,
			PublishedText:  post.PublishedText,
			PublishedAt:    post.PublishedAt,
			LikeCount:      post.LikeCount,
			CommentCount:   post.CommentCount,
			Images:         thumbnails(post.Images),
			VideoID:        post.VideoID,
		})
	}

	return contract.CommunityPayloadV1{
		ChannelID: channelID,
		Posts:     mapped,
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID:  channelID,
			MaxResults: maxResults,
			PageCount:  page.PageCount,
			CursorEnd:  page.CursorEnd,
			Exhausted:  page.Exhausted,
		},
	}
}

func videoListPayload(channelID string, items []youtubejs.ContentItem, maxResults int, page *youtubejs.Pagination, shorts bool) (contract.VideoListV1, contract.ShortsListV1) {
	videos := make([]contract.VideoListItemV1, 0, len(items))
	for _, item := range items {
		videos = append(videos, contract.VideoListItemV1{
			VideoID:      item.VideoID,
			ChannelID:    channelID,
			Title:        item.Title,
			PublishedAt:  item.PublishedAt,
			ScheduledFor: item.ScheduledFor,
			IsPremiere:   item.IsPremiere,
		})
	}

	if shorts {
		return contract.VideoListV1{}, contract.ShortsListV1{
			ChannelID: channelID,
			Videos:    videos,
			Coverage: contract.ShortsListCoverageV1{
				ChannelID:   channelID,
				MaxResults:  maxResults,
				CursorStart: page.CursorStart,
				CursorEnd:   page.CursorEnd,
				Exhausted:   page.Exhausted,
			},
		}
	}

	return contract.VideoListV1{
		ChannelID: channelID,
		Videos:    videos,
		Coverage: contract.ChannelListCoverageV1{
			ChannelID:   channelID,
			MaxResults:  maxResults,
			CursorStart: page.CursorStart,
			CursorEnd:   page.CursorEnd,
			Exhausted:   page.Exhausted,
		},
	}, contract.ShortsListV1{}
}

func liveSnapshotPayload(channelID string, sessions []youtubejs.LiveSessionItem, includeMetadata bool) contract.LiveSnapshotV1 {
	mapped := make([]contract.LiveSessionV1, 0, len(sessions))
	statuses := make([]string, 0, 4)
	seenStatus := make(map[string]struct{}, 4)

	for _, session := range sessions {
		item := contract.LiveSessionV1{
			VideoID:     session.VideoID,
			ChannelID:   channelID,
			Status:      session.Status,
			ScheduledAt: session.ScheduledAt,
			StartedAt:   session.StartedAt,
			EndedAt:     session.EndedAt,
		}

		if includeMetadata {
			item.Title = session.Title
			item.ThumbnailURL = session.ThumbnailURL
		}

		mapped = append(mapped, item)

		if _, ok := seenStatus[session.Status]; ok {
			continue
		}

		seenStatus[session.Status] = struct{}{}
		statuses = append(statuses, session.Status)
	}

	if len(statuses) == 0 {
		statuses = []string{"LIVE", "UPCOMING"}
	}

	return contract.LiveSnapshotV1{
		Sessions: mapped,
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{channelID},
			GroupKey:            channelID,
			Filters:             contract.LiveFiltersV1{Statuses: statuses},
		},
	}
}

func channelStatsPayload(channelID string, stats youtubejs.ChannelStatsItem) (contract.ChannelStatsV1, bool) {
	fields := make([]string, 0, 3)

	if stats.SubscriberCount != nil {
		fields = append(fields, "subscriber_count")
	}

	if stats.ViewCount != nil {
		fields = append(fields, "view_count")
	}

	if stats.VideoCount != nil {
		fields = append(fields, "video_count")
	}

	if len(fields) == 0 {
		return contract.ChannelStatsV1{}, false
	}

	return contract.ChannelStatsV1{
		ChannelID:       channelID,
		SubscriberCount: stats.SubscriberCount,
		ViewCount:       stats.ViewCount,
		VideoCount:      stats.VideoCount,
		Coverage: contract.ChannelStatsCoverageV1{
			ChannelID: channelID,
			Fields:    fields,
		},
	}, true
}

func channelProfilePayload(channelID string, profile youtubejs.ChannelProfileItem) (contract.ChannelProfileV1, bool) {
	payload := contract.ChannelProfileV1{ChannelID: channelID}
	fields := make([]string, 0, 4)

	if profile.Handle != nil {
		payload.Handle = contract.FieldValue[string]{Present: true, Value: *profile.Handle}

		fields = append(fields, "handle")
	}

	if profile.Description != nil {
		payload.Description = contract.FieldValue[string]{Present: true, Value: *profile.Description}

		fields = append(fields, "description")
	}

	if profile.Country != nil {
		payload.Country = contract.FieldValue[string]{Present: true, Value: *profile.Country}

		fields = append(fields, "country")
	}

	if profile.JoinedDate != nil {
		payload.JoinedDate = contract.FieldValue[string]{Present: true, Value: *profile.JoinedDate}

		fields = append(fields, "joined_date")
	}

	if len(fields) == 0 {
		return contract.ChannelProfileV1{}, false
	}

	payload.Coverage = contract.ChannelProfileCoverageV1{ChannelID: channelID, Fields: fields}

	return payload, true
}

func channelPhotoPayload(channelID string, variants []youtubejs.ChannelPhotoVariant) (contract.ChannelPhotoV1, bool) {
	if len(variants) == 0 {
		return contract.ChannelPhotoV1{}, false
	}

	mapped := make([]contract.PhotoVariantV1, 0, len(variants))
	kinds := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)

	for _, variant := range variants {
		mapped = append(mapped, contract.PhotoVariantV1{
			Kind:   variant.Kind,
			URL:    variant.URL,
			Width:  variant.Width,
			Height: variant.Height,
		})
		if _, ok := seen[variant.Kind]; ok {
			continue
		}

		seen[variant.Kind] = struct{}{}
		kinds = append(kinds, variant.Kind)
	}

	return contract.ChannelPhotoV1{
		ChannelID: channelID,
		Variants:  mapped,
		Coverage: contract.ChannelPhotoCoverageV1{
			ChannelID: channelID,
			Variants:  kinds,
		},
	}, true
}

func viewerPayload(videoID string, result *youtubejs.ViewerResult, windowStart time.Time, windowSeconds int) contract.ViewerSampleV1 {
	return contract.ViewerSampleV1{
		VideoID:             videoID,
		ViewerCount:         result.ViewerCount,
		Availability:        result.Availability,
		SampleWindowStart:   windowStart,
		SampleWindowSeconds: windowSeconds,
		Coverage: contract.ViewerSampleCoverageV1{
			VideoID:             videoID,
			SampleWindowStart:   windowStart,
			SampleWindowSeconds: windowSeconds,
		},
	}
}

func thumbnails(values []parser.Thumbnail) []contract.Thumbnail {
	if len(values) == 0 {
		return nil
	}

	mapped := make([]contract.Thumbnail, 0, len(values))
	for _, value := range values {
		mapped = append(mapped, contract.Thumbnail{URL: value.URL, Width: value.Width, Height: value.Height})
	}

	return mapped
}
