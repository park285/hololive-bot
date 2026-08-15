package content

import (
	"encoding/json"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func (c coverageValue) covers(entity Entity) bool {
	item := entity.item()
	if c.Videos != nil {
		return contract.ChannelListCoversVideo(c.Videos, &item)
	}
	if c.Shorts != nil {
		return contract.ShortsListCoversVideo(*c.Shorts, item)
	}
	return false
}

func (c coverageValue) relationTo(evidence coverageValue) contract.CoverageRelation {
	if c.Videos != nil && evidence.Videos != nil {
		return contract.RelateChannelList(c.Videos, evidence.Videos)
	}
	if c.Shorts != nil && evidence.Shorts != nil {
		return contract.RelateShortsList(*c.Shorts, *evidence.Shorts)
	}
	return contract.CoverageDisjoint
}

func (e Entity) item() contract.VideoListItemV1 {
	return contract.VideoListItemV1{
		VideoID:      e.VideoID,
		ChannelID:    e.ChannelID,
		Title:        e.Title,
		PublishedAt:  e.PublishedAt,
		ScheduledFor: e.ScheduledFor,
	}
}

func valueDigest(entity *Entity) string {
	payload, err := json.Marshal(struct {
		Title        string     `json:"title"`
		PublishedAt  *time.Time `json:"published_at,omitempty"`
		ScheduledFor *time.Time `json:"scheduled_for,omitempty"`
	}{Title: entity.Title, PublishedAt: entity.PublishedAt, ScheduledFor: entity.ScheduledFor})
	if err != nil {
		return ""
	}
	return contract.SHA256Hex(payload)
}

func coverageBytes(value coverageValue) []byte {
	var payload any
	switch {
	case value.Videos != nil:
		payload = value.Videos
	case value.Shorts != nil:
		payload = value.Shorts
	default:
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}

func ParseCoverage(kind contract.ObservationKind, raw []byte) (coverageValue, error) {
	if kind == contract.KindShortsList {
		var coverage contract.ShortsListCoverageV1
		if err := json.Unmarshal(raw, &coverage); err != nil {
			return coverageValue{}, fmt.Errorf("decode shorts coverage: %w", err)
		}
		return ShortsCoverage(&coverage), nil
	}
	var coverage contract.ChannelListCoverageV1
	if err := json.Unmarshal(raw, &coverage); err != nil {
		return coverageValue{}, fmt.Errorf("decode video coverage: %w", err)
	}
	return VideoCoverage(&coverage), nil
}

func MarshalCoverage(value coverageValue) ([]byte, error) {
	raw := coverageBytes(value)
	if raw == nil {
		return nil, fmt.Errorf("content coverage is empty")
	}
	return raw, nil
}
