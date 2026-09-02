package holodexcollector

import (
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func uniqueStatsCount(rows []parsedLive, field string, valueOf func(*liveChannel) *int64) (*int64, error) {
	var selected *int64

	for i := range rows {
		value := valueOf(&rows[i].row.Channel)
		if value == nil {
			continue
		}

		if selected != nil && *selected != *value {
			return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex channel "+field+" metadata conflicts across rows")
		}

		copied := *value

		selected = &copied
	}

	return selected, nil
}

func photoPayload(channelID string, rows []parsedLive) (contract.ChannelPhotoV1, bool, error) {
	var selected string

	for i := range rows {
		row := &rows[i]
		photoURL, ok := httpsURL(row.row.Channel.Photo)

		if !ok {
			continue
		}

		if selected != "" && selected != photoURL {
			return contract.ChannelPhotoV1{}, false, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "holodex channel photo metadata conflicts across rows")
		}

		selected = photoURL
	}

	if selected == "" {
		return contract.ChannelPhotoV1{}, false, nil
	}

	return contract.ChannelPhotoV1{
		ChannelID: channelID,
		Variants:  []contract.PhotoVariantV1{{Kind: "avatar", URL: selected}},
		Coverage: contract.ChannelPhotoCoverageV1{
			ChannelID: channelID,
			Variants:  []string{"avatar"},
		},
	}, true, nil
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
