package officialcollector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const (
	officialScheduleSubject = "global:hololive-schedule"
	officialDatetimeLayout  = "2006/01/02 15:04:05"
)

var officialScheduleJST = time.FixedZone("Asia/Tokyo", 9*60*60)

type scheduleVideoRow struct {
	Datetime string `json:"datetime"`
	IsLive   bool   `json:"isLive"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Name     string `json:"name"`
	Talent   struct {
		Name string `json:"name"`
	} `json:"talent"`
}

func parseScheduleSnapshot(body []byte) (contract.ScheduleSnapshotV1, error) {
	items, err := parseScheduleItems(body)
	if err != nil {
		return contract.ScheduleSnapshotV1{}, err
	}
	return contract.ScheduleSnapshotV1{
		GroupKey: officialScheduleSubject,
		Items:    items,
		Coverage: contract.ScheduleCoverageV1{GroupKey: officialScheduleSubject},
	}, nil
}

func parseScheduleItems(body []byte) ([]contract.ScheduleItemV1, error) {
	trimmed := bytes.TrimSpace(body)
	if !json.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, collecterr.New(collecterr.ParserDrift, "official schedule response is not a JSON object")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode official schedule: %w", err))
	}
	rawGroups, ok := root["dateGroupList"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawGroups), []byte("null")) {
		return nil, collecterr.New(collecterr.ParserDrift, "official schedule dateGroupList is missing")
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(rawGroups, &groups); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode official schedule dateGroupList: %w", err))
	}
	items := make([]contract.ScheduleItemV1, 0)
	seen := make(map[string]struct{})
	for index, rawGroup := range groups {
		groupItems, err := parseScheduleGroup(rawGroup, index, seen)
		if err != nil {
			return nil, err
		}
		items = append(items, groupItems...)
	}
	return items, nil
}

func parseScheduleGroup(rawGroup json.RawMessage, index int, seen map[string]struct{}) ([]contract.ScheduleItemV1, error) {
	var group map[string]json.RawMessage
	if err := json.Unmarshal(rawGroup, &group); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode official schedule group %d: %w", index, err))
	}
	rawRows, ok := group["videoList"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawRows), []byte("null")) {
		return nil, collecterr.New(collecterr.ParserDrift, "official schedule videoList is missing")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(rawRows, &rows); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode official schedule videoList: %w", err))
	}
	items := make([]contract.ScheduleItemV1, 0, len(rows))
	for _, rawRow := range rows {
		item, err := parseScheduleRow(rawRow)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[item.ExternalID]; exists {
			continue
		}
		seen[item.ExternalID] = struct{}{}
		items = append(items, item)
	}
	return items, nil
}

func parseScheduleRow(rawRow json.RawMessage) (contract.ScheduleItemV1, error) {
	var row scheduleVideoRow
	if err := json.Unmarshal(rawRow, &row); err != nil {
		return contract.ScheduleItemV1{}, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode official schedule row: %w", err))
	}
	videoID, err := parseScheduleVideoID(row.URL)
	if err != nil {
		return contract.ScheduleItemV1{}, err
	}
	scheduledAt, err := time.ParseInLocation(officialDatetimeLayout, strings.TrimSpace(row.Datetime), officialScheduleJST)
	if err != nil {
		return contract.ScheduleItemV1{}, collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("parse official schedule datetime: %w", err))
	}
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = strings.TrimSpace(row.Name)
	}
	if title == "" {
		title = strings.TrimSpace(row.Talent.Name)
	}
	if title == "" {
		return contract.ScheduleItemV1{}, collecterr.New(collecterr.ParserDrift, "official schedule row title is empty")
	}
	return contract.ScheduleItemV1{
		ExternalID:  videoID,
		VideoID:     videoID,
		Title:       title,
		ScheduledAt: scheduledAt.UTC(),
		IsLive:      row.IsLive,
	}, nil
}

func parseScheduleVideoID(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("parse official schedule url: %w", err))
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "https" || (host != "youtube.com" && host != "www.youtube.com") || parsed.Path != "/watch" {
		return "", collecterr.New(collecterr.ParserDrift, "official schedule url is not a YouTube watch URL")
	}
	videoID := strings.TrimSpace(parsed.Query().Get("v"))
	if !validVideoID(videoID) {
		return "", collecterr.New(collecterr.ParserDrift, "official schedule video id is invalid")
	}
	return videoID, nil
}

func validVideoID(videoID string) bool {
	if videoID == "" || len(videoID) > 128 {
		return false
	}
	for _, char := range videoID {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}
