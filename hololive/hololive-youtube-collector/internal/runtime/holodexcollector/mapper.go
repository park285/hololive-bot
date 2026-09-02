package holodexcollector

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/url"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

const officialScheduleSubject = "global:hololive-schedule"

type liveRow struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	TopicID        string         `json:"topic_id"`
	Thumbnail      string         `json:"thumbnail"`
	ChannelID      string         `json:"channel_id"`
	Status         string         `json:"status"`
	StartScheduled string         `json:"start_scheduled"`
	StartActual    string         `json:"start_actual"`
	EndActual      string         `json:"end_actual"`
	LiveViewers    jsontext.Value `json:"live_viewers"`
	Channel        liveChannel    `json:"channel"`
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
	rawRows, err := decodeLiveRows(body)
	if err != nil {
		return nil, fmt.Errorf("decode live rows: %w", err)
	}

	rows := make([]parsedLive, 0, len(rawRows))
	seen := make(map[string]struct{}, len(rawRows))
	validRows := 0

	for _, raw := range rawRows {
		row, rowErr := parseLiveRow(raw)
		if rowErr != nil {
			continue
		}

		validRows++

		if _, exists := seen[row.row.ID]; exists {
			continue
		}

		seen[row.row.ID] = struct{}{}
		rows = append(rows, row)
	}

	if len(rawRows) > 0 && validRows == 0 {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex live response has no valid rows")
	}

	return rows, nil
}

func decodeLiveRows(body []byte) ([]jsontext.Value, error) {
	trimmed := bytes.TrimSpace(body)
	if !jsontext.Value(trimmed).IsValid() || len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex live response is not a JSON array")
	}

	var rawRows []jsontext.Value

	if err := jsonv2.Unmarshal(trimmed, &rawRows); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode holodex live: %w", err))
	}

	return rawRows, nil
}

func parseLiveRow(raw jsontext.Value) (parsedLive, error) {
	var row liveRow

	if err := jsonv2.Unmarshal(raw, &row); err != nil {
		return parsedLive{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode holodex live row: %w", err))
	}

	if strings.TrimSpace(row.ID) == "" {
		return parsedLive{}, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex live row id is empty")
	}

	status, err := mapLiveStatus(row.Status)
	if err != nil {
		return parsedLive{}, fmt.Errorf("map live status: %w", err)
	}

	channelID, err := liveChannelID(&row)
	if err != nil {
		return parsedLive{}, fmt.Errorf("live channel ID: %w", err)
	}

	scheduled, started, ended, err := parseLiveTimes(&row)
	if err != nil {
		return parsedLive{}, fmt.Errorf("parse live times: %w", err)
	}

	parsed := parsedLive{
		row: row, channelID: channelID, status: status,
		scheduled: scheduled, started: started, ended: ended,
	}
	if _, _, err := viewerAvailability(&parsed); err != nil {
		return parsedLive{}, fmt.Errorf("viewer availability: %w", err)
	}

	return parsed, nil
}

func liveChannelID(row *liveRow) (string, error) {
	channelID := strings.TrimSpace(row.ChannelID)
	nestedChannelID := strings.TrimSpace(row.Channel.ID)

	if channelID != "" && nestedChannelID != "" && channelID != nestedChannelID {
		return "", collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex live row has conflicting channel identity")
	}

	if channelID == "" {
		channelID = nestedChannelID
	}

	if channelID == "" {
		return "", collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex live row channel id is empty")
	}

	return channelID, nil
}

func parseLiveTimes(row *liveRow) (scheduled, started, ended *time.Time, err error) {
	scheduled, err = parseOptionalTime(row.StartScheduled)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse optional time: %w", err)
	}

	started, err = parseOptionalTime(row.StartActual)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse optional time: %w", err)
	}

	ended, err = parseOptionalTime(row.EndActual)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse optional time: %w", err)
	}

	return scheduled, started, ended, nil
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
		unsupported := collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex live status is unsupported")

		return "", fmt.Errorf("map live status: %w", unsupported)
	}
}

//nolint:nilnil // 빈 값은 nullable 컬럼에 그대로 실리는 "값 없음"이라 nil 시각이 정상 결과다.
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
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("parse holodex timestamp: %w", err))
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

func subjectAllowed(input *collectutil.RunInput, kind contract.ObservationKind, subject string) (bool, error) {
	out, err := input.Allows(kind, subject)
	if err != nil {
		return out, fmt.Errorf("allows: %w", err)
	}

	return out, nil
}

func viewerPayload(row *parsedLive, windowStart time.Time, windowSeconds int) (contract.ViewerSampleV1, error) {
	availability, count, err := viewerAvailability(row)
	if err != nil {
		return contract.ViewerSampleV1{}, fmt.Errorf("viewer availability: %w", err)
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

//nolint:nilnil // 시청자 수가 없는 상태(HIDDEN/UNAVAILABLE)는 availability로 표현되며 오류가 아니다.
func viewerAvailability(row *parsedLive) (availability string, viewerCount *int64, parseErr error) {
	raw := bytes.TrimSpace(row.row.LiveViewers)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		if row.status == "LIVE" {
			return "HIDDEN", nil, nil
		}

		return "UNAVAILABLE", nil, nil
	}

	var count int64

	if err := jsonv2.Unmarshal(raw, &count); err != nil {
		return "", nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode holodex viewer count: %w", err))
	}

	if count < 0 {
		return "", nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex viewer count is negative")
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

func livePayload(channelID string, sessions []parsedLive, includeMetadata bool) contract.LiveSnapshotV1 {
	mapped := make([]contract.LiveSessionV1, 0, len(sessions))
	statuses := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)

	for i := range sessions {
		session := &sessions[i]

		item := contract.LiveSessionV1{
			VideoID:     session.row.ID,
			ChannelID:   channelID,
			Status:      session.status,
			ScheduledAt: session.scheduled,
			StartedAt:   session.started,
			EndedAt:     session.ended,
		}

		if includeMetadata {
			item.Title = strings.TrimSpace(session.row.Title)
			item.TopicID = strings.TrimSpace(session.row.TopicID)
			item.ThumbnailURL, _ = httpsURL(session.row.Thumbnail)
		}

		mapped = append(mapped, item)

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

func statsPayload(channelID string, rows []parsedLive) (contract.ChannelStatsV1, bool, error) {
	subscriber, err := uniqueStatsCount(rows, "subscriber_count", func(channel *liveChannel) *int64 {
		return channel.SubscriberCount
	})
	if err != nil {
		return contract.ChannelStatsV1{}, false, fmt.Errorf("unique stats count: %w", err)
	}

	videos, err := uniqueStatsCount(rows, "video_count", func(channel *liveChannel) *int64 {
		return channel.VideoCount
	})
	if err != nil {
		return contract.ChannelStatsV1{}, false, fmt.Errorf("unique stats count: %w", err)
	}

	fields := make([]string, 0, 2)

	if subscriber != nil {
		fields = append(fields, "subscriber_count")
	}

	if videos != nil {
		fields = append(fields, "video_count")
	}

	if len(fields) == 0 {
		return contract.ChannelStatsV1{}, false, nil
	}

	return contract.ChannelStatsV1{
		ChannelID:       channelID,
		SubscriberCount: subscriber,
		VideoCount:      videos,
		Coverage: contract.ChannelStatsCoverageV1{
			ChannelID: channelID,
			Fields:    fields,
		},
	}, true, nil
}
