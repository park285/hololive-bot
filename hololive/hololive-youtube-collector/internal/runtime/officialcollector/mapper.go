package officialcollector

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

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
	CollaboTalents jsontext.Value `json:"collaboTalents"`
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
	groups, err := decodeScheduleGroups(body)
	if err != nil {
		return nil, err
	}
	items := make([]contract.ScheduleItemV1, 0)
	seen := make(map[string]struct{})
	totalRows := 0
	validRows := 0
	for index, rawGroup := range groups {
		groupItems, rowCount, validCount, err := parseScheduleGroup(rawGroup, index, seen)
		if err != nil {
			return nil, err
		}
		totalRows += rowCount
		validRows += validCount
		items = append(items, groupItems...)
	}
	if totalRows > 0 && validRows == 0 {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule has no valid rows")
	}
	return items, nil
}

func decodeScheduleGroups(body []byte) ([]jsontext.Value, error) {
	trimmed := bytes.TrimSpace(body)
	if !jsontext.Value(trimmed).IsValid() || len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule response is not a JSON object")
	}
	var root map[string]jsontext.Value
	if err := jsonv2.Unmarshal(trimmed, &root); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode official schedule: %w", err))
	}
	rawGroups, ok := root["dateGroupList"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawGroups), []byte("null")) {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule dateGroupList is missing")
	}
	var groups []jsontext.Value
	if err := jsonv2.Unmarshal(rawGroups, &groups); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode official schedule dateGroupList: %w", err))
	}
	return groups, nil
}

func parseScheduleGroup(
	rawGroup jsontext.Value,
	index int,
	seen map[string]struct{},
) (items []contract.ScheduleItemV1, rowCount, validCount int, err error) {
	rows, err := decodeScheduleRows(rawGroup, index)
	if err != nil {
		return nil, 0, 0, err
	}
	items = make([]contract.ScheduleItemV1, 0, len(rows))
	for _, rawRow := range rows {
		item, err := parseScheduleRow(rawRow)
		if err != nil {
			continue
		}
		validCount++
		if _, exists := seen[item.ExternalID]; exists {
			continue
		}
		seen[item.ExternalID] = struct{}{}
		items = append(items, item)
	}
	return items, len(rows), validCount, nil
}

func decodeScheduleRows(rawGroup jsontext.Value, index int) ([]jsontext.Value, error) {
	var group map[string]jsontext.Value
	if err := jsonv2.Unmarshal(rawGroup, &group); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode official schedule group %d: %w", index, err))
	}
	rawRows, ok := group["videoList"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawRows), []byte("null")) {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule videoList is missing")
	}
	var rows []jsontext.Value
	if err := jsonv2.Unmarshal(rawRows, &rows); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode official schedule videoList: %w", err))
	}
	return rows, nil
}

func parseScheduleRow(rawRow jsontext.Value) (contract.ScheduleItemV1, error) {
	var row scheduleVideoRow
	if err := jsonv2.Unmarshal(rawRow, &row); err != nil {
		return contract.ScheduleItemV1{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode official schedule row: %w", err))
	}
	videoID, err := parseScheduleVideoID(row.URL)
	if err != nil {
		return contract.ScheduleItemV1{}, err
	}
	scheduledAt, err := time.ParseInLocation(officialDatetimeLayout, strings.TrimSpace(row.Datetime), officialScheduleJST)
	if err != nil {
		return contract.ScheduleItemV1{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("parse official schedule datetime: %w", err))
	}
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = strings.TrimSpace(row.Name)
	}
	if title == "" {
		title = strings.TrimSpace(row.Talent.Name)
	}
	if title == "" {
		return contract.ScheduleItemV1{}, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule row title is empty")
	}
	collaboTalentNames, err := parseCollaboTalentNames(row.CollaboTalents, row.Name, row.Talent.Name)
	if err != nil {
		return contract.ScheduleItemV1{}, err
	}
	return contract.ScheduleItemV1{
		ExternalID:         videoID,
		VideoID:            videoID,
		Title:              title,
		ScheduledAt:        scheduledAt.UTC(),
		IsLive:             row.IsLive,
		CollaboTalentNames: collaboTalentNames,
	}, nil
}

func parseCollaboTalentNames(raw jsontext.Value, hostNames ...string) ([]string, error) {
	names, err := decodeCollaboTalentNames(raw)
	if err != nil {
		return nil, err
	}
	return collectCollaboTalentNames(names, collaboHostSkipSet(hostNames))
}

func decodeCollaboTalentNames(raw jsontext.Value) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var talents []struct {
		Name string `json:"name"`
	}
	if err := jsonv2.Unmarshal(trimmed, &talents); err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("decode official schedule collaboTalents: %w", err))
	}
	names := make([]string, 0, len(talents))
	for _, talent := range talents {
		names = append(names, talent.Name)
	}
	return names, nil
}

func collaboHostSkipSet(hostNames []string) map[string]struct{} {
	skipped := make(map[string]struct{}, len(hostNames))
	for _, name := range hostNames {
		if key := stringutil.Normalize(name); key != "" {
			skipped[key] = struct{}{}
		}
	}
	return skipped
}

func collectCollaboTalentNames(names []string, skipped map[string]struct{}) ([]string, error) {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		collected, ok, err := collectCollaboTalentName(name, skipped, seen)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, collected)
		if len(out) > contract.MaxScheduleCollaboTalentNames {
			return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule collaboTalents exceeds contract limit")
		}
	}
	return out, nil
}

func collectCollaboTalentName(name string, skipped, seen map[string]struct{}) (collected string, include bool, err error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", false, nil
	}
	if len(trimmed) > contract.MaxScheduleCollaboTalentNameBytes {
		return "", false, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule collabo talent name exceeds contract limit")
	}
	key := stringutil.Normalize(trimmed)
	if _, skip := skipped[key]; skip {
		return "", false, nil
	}
	if _, exists := seen[key]; exists {
		return "", false, nil
	}
	seen[key] = struct{}{}
	return trimmed, true, nil
}

func parseScheduleVideoID(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, fmt.Errorf("parse official schedule url: %w", err))
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "https" || (host != "youtube.com" && host != "www.youtube.com") || parsed.Path != "/watch" {
		return "", collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule url is not a YouTube watch URL")
	}
	videoID := strings.TrimSpace(parsed.Query().Get("v"))
	if !validVideoID(videoID) {
		return "", collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "official schedule video id is invalid")
	}
	return videoID, nil
}

func validVideoID(videoID string) bool {
	if videoID == "" || len(videoID) > 128 {
		return false
	}
	for _, char := range videoID {
		if !validVideoIDChar(char) {
			return false
		}
	}
	return true
}

func validVideoIDChar(char rune) bool {
	if char >= 'a' && char <= 'z' {
		return true
	}
	if char >= 'A' && char <= 'Z' {
		return true
	}
	if char >= '0' && char <= '9' {
		return true
	}
	return char == '-' || char == '_'
}
