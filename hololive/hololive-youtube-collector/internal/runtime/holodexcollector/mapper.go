package holodexcollector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

const officialScheduleSubject = "global:hololive-schedule"

type liveRow struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	ChannelID      string          `json:"channel_id"`
	Status         string          `json:"status"`
	StartScheduled string          `json:"start_scheduled"`
	StartActual    string          `json:"start_actual"`
	EndActual      string          `json:"end_actual"`
	LiveViewers    json.RawMessage `json:"live_viewers"`
	Channel        liveChannel     `json:"channel"`
}

type liveChannel struct {
	ID              string `json:"id"`
	Photo           string `json:"photo"`
	SubscriberCount *int64 `json:"subscriber_count"`
	VideoCount      *int64 `json:"video_count"`
	Name            string `json:"name"`
}

type parsedLive struct {
	row       liveRow
	channelID string
	status    string
	scheduled *time.Time
	started   *time.Time
	ended     *time.Time
}

func parseLiveRows(body []byte) ([]parsedLive, error) {
	trimmed := bytes.TrimSpace(body)
	if !json.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, collecterr.New(collecterr.ParserDrift, "holodex live response is not a JSON array")
	}
	var rawRows []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawRows); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode holodex live: %w", err))
	}
	rows := make([]parsedLive, 0, len(rawRows))
	seen := make(map[string]struct{}, len(rawRows))
	for _, raw := range rawRows {
		row, err := appendUniqueLiveRow(raw, seen)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func appendUniqueLiveRow(raw json.RawMessage, seen map[string]struct{}) (parsedLive, error) {
	row, err := parseLiveRow(raw)
	if err != nil {
		return parsedLive{}, err
	}
	if _, exists := seen[row.row.ID]; exists {
		return parsedLive{}, collecterr.New(collecterr.ParserDrift, "holodex live response has duplicate video id")
	}
	seen[row.row.ID] = struct{}{}
	return row, nil
}

func parseLiveRow(raw json.RawMessage) (parsedLive, error) {
	var row liveRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return parsedLive{}, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode holodex live row: %w", err))
	}
	if strings.TrimSpace(row.ID) == "" {
		return parsedLive{}, collecterr.New(collecterr.ParserDrift, "holodex live row id is empty")
	}
	status, err := mapLiveStatus(row.Status)
	if err != nil {
		return parsedLive{}, err
	}
	channelID := strings.TrimSpace(row.ChannelID)
	if channelID == "" {
		channelID = strings.TrimSpace(row.Channel.ID)
	}
	if channelID == "" {
		return parsedLive{}, collecterr.New(collecterr.ParserDrift, "holodex live row channel id is empty")
	}
	scheduled, err := parseOptionalTime(row.StartScheduled)
	if err != nil {
		return parsedLive{}, err
	}
	started, err := parseOptionalTime(row.StartActual)
	if err != nil {
		return parsedLive{}, err
	}
	ended, err := parseOptionalTime(row.EndActual)
	if err != nil {
		return parsedLive{}, err
	}
	return parsedLive{
		row: row, channelID: channelID, status: status,
		scheduled: scheduled, started: started, ended: ended,
	}, nil
}

func mapLiveStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "live":
		return "LIVE", nil
	case "upcoming":
		return "UPCOMING", nil
	case "past":
		return "ENDED", nil
	default:
		return "", collecterr.New(collecterr.ParserDrift, "holodex live status is unsupported")
	}
}

func parseOptionalTime(raw string) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("parse holodex timestamp: %w", err))
	}
	utc := parsed.UTC()
	return &utc, nil
}

func requestedSet(ids []string) map[string]struct{} {
	result := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result
}

func subjectAllowed(input *collectutil.RunInput, kind contract.ObservationKind, subject string) bool {
	subjects, ok := input.EnabledSubjects[kind]
	if !ok {
		return true
	}
	return slices.Contains(subjects, subject)
}

func viewerPayload(row *parsedLive, windowStart time.Time, windowSeconds int) (contract.ViewerSampleV1, error) {
	availability, count, err := viewerAvailability(row)
	if err != nil {
		return contract.ViewerSampleV1{}, err
	}
	return contract.ViewerSampleV1{
		VideoID:             row.row.ID,
		ViewerCount:         count,
		Availability:        availability,
		SampleWindowStart:   windowStart,
		SampleWindowSeconds: windowSeconds,
		Coverage: contract.ViewerSampleCoverageV1{
			VideoID:             row.row.ID,
			SampleWindowStart:   windowStart,
			SampleWindowSeconds: windowSeconds,
		},
	}, nil
}

func viewerAvailability(row *parsedLive) (availability string, viewerCount *int64, parseErr error) {
	raw := bytes.TrimSpace(row.row.LiveViewers)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		if row.status == "LIVE" {
			return "HIDDEN", nil, nil
		}
		return "UNAVAILABLE", nil, nil
	}
	var count int64
	if err := json.Unmarshal(raw, &count); err != nil {
		return "", nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode holodex viewer count: %w", err))
	}
	if count < 0 {
		return "", nil, collecterr.New(collecterr.ParserDrift, "holodex viewer count is negative")
	}
	return "AVAILABLE", &count, nil
}

func httpsURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	return parsed.String(), true
}

func livePayload(channelID string, sessions []parsedLive) contract.LiveSnapshotV1 {
	mapped := make([]contract.LiveSessionV1, 0, len(sessions))
	statuses := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	for i := range sessions {
		session := &sessions[i]
		mapped = append(mapped, contract.LiveSessionV1{
			VideoID:     session.row.ID,
			ChannelID:   channelID,
			Status:      session.status,
			ScheduledAt: session.scheduled,
			StartedAt:   session.started,
			EndedAt:     session.ended,
		})
		if _, ok := seen[session.status]; ok {
			continue
		}
		seen[session.status] = struct{}{}
		statuses = append(statuses, session.status)
	}
	return contract.LiveSnapshotV1{
		Sessions: mapped,
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{channelID},
			Filters:             contract.LiveFiltersV1{Statuses: statuses},
		},
	}
}

func statsPayload(channelID string, rows []parsedLive) (contract.ChannelStatsV1, bool) {
	subscriber, videos := firstStatsCounts(rows)
	fields := make([]string, 0, 2)
	if subscriber != nil {
		fields = append(fields, "subscriber_count")
	}
	if videos != nil {
		fields = append(fields, "video_count")
	}
	if len(fields) == 0 {
		return contract.ChannelStatsV1{}, false
	}
	return contract.ChannelStatsV1{
		ChannelID:       channelID,
		SubscriberCount: subscriber,
		VideoCount:      videos,
		Coverage: contract.ChannelStatsCoverageV1{
			ChannelID: channelID,
			Fields:    fields,
		},
	}, true
}

func firstStatsCounts(rows []parsedLive) (subscriberCount, viewCount *int64) {
	var subscriber *int64
	var videos *int64
	for i := range rows {
		row := &rows[i]
		if subscriber == nil && row.row.Channel.SubscriberCount != nil {
			subscriber = row.row.Channel.SubscriberCount
		}
		if videos == nil && row.row.Channel.VideoCount != nil {
			videos = row.row.Channel.VideoCount
		}
	}
	return subscriber, videos
}

func photoPayload(channelID string, rows []parsedLive) (contract.ChannelPhotoV1, bool) {
	for i := range rows {
		row := &rows[i]
		photoURL, ok := httpsURL(row.row.Channel.Photo)
		if !ok {
			continue
		}
		return contract.ChannelPhotoV1{
			ChannelID: channelID,
			Variants:  []contract.PhotoVariantV1{{Kind: "avatar", URL: photoURL}},
			Coverage: contract.ChannelPhotoCoverageV1{
				ChannelID: channelID,
				Variants:  []string{"avatar"},
			},
		}, true
	}
	return contract.ChannelPhotoV1{}, false
}

func schedulePayload(rows []parsedLive, allowed map[string]struct{}) contract.ScheduleSnapshotV1 {
	items := make([]contract.ScheduleItemV1, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		if item, ok := scheduleItemFromRow(&rows[i], allowed, seen); ok {
			items = append(items, item)
		}
	}
	return contract.ScheduleSnapshotV1{
		GroupKey: officialScheduleSubject,
		Items:    items,
		Coverage: contract.ScheduleCoverageV1{GroupKey: officialScheduleSubject},
	}
}

func scheduleItemFromRow(row *parsedLive, allowed, seen map[string]struct{}) (contract.ScheduleItemV1, bool) {
	if _, ok := allowed[row.channelID]; !ok || row.scheduled == nil {
		return contract.ScheduleItemV1{}, false
	}
	if _, exists := seen[row.row.ID]; exists {
		return contract.ScheduleItemV1{}, false
	}
	title := scheduleTitle(row)
	if title == "" {
		return contract.ScheduleItemV1{}, false
	}
	seen[row.row.ID] = struct{}{}
	return contract.ScheduleItemV1{
		ExternalID:  row.row.ID,
		VideoID:     row.row.ID,
		ChannelID:   row.channelID,
		Title:       title,
		ScheduledAt: *row.scheduled,
		IsLive:      row.status == "LIVE",
	}, true
}

func scheduleTitle(row *parsedLive) string {
	title := strings.TrimSpace(row.row.Title)
	if title != "" {
		return title
	}
	return strings.TrimSpace(row.row.Channel.Name)
}
