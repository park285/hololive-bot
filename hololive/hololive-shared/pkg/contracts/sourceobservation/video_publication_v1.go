package sourceobservation

import (
	"errors"
	"fmt"
	"time"
)

// VideoListPublicationContractGeneration은 항목별 player 단건 공개 근거(Publication)를 담는 video_list 계약 세대입니다.
const VideoListPublicationContractGeneration int64 = 2

// VideoPublicationStatus는 player 단건 응답에서 얻은 신규성 근거의 종류입니다.
type VideoPublicationStatus string

const (
	// VideoPublicationPublished는 player microformat publishDate가 시·분·초와 offset을 가진 RFC3339일 때만 성립합니다.
	VideoPublicationPublished VideoPublicationStatus = "PUBLISHED"
	// VideoPublicationUpcomingPremiere는 live 콘텐츠가 아닌 예정 영상이고 대기 상태의 예정 시각이 확인된 경우입니다.
	VideoPublicationUpcomingPremiere VideoPublicationStatus = "UPCOMING_PREMIERE"
	// VideoPublicationUnresolved는 응답은 받았지만 결정적인 공개·예정 시각이 없는 경우입니다. 신규성 근거가 아닙니다.
	VideoPublicationUnresolved VideoPublicationStatus = "UNRESOLVED"
)

// VideoPublicationV1은 목록 lockup이 아니라 같은 영상의 player 단건 응답 하나에서 만든 공개 근거입니다.
// CheckedAt은 그 응답을 받은 collector 시각이며, 이전 응답의 근거를 재사용해도 원래 확인 시각을 유지합니다.
type VideoPublicationV1 struct {
	Status       VideoPublicationStatus `json:"status"`
	PublishedAt  *time.Time             `json:"published_at,omitempty"`
	ScheduledFor *time.Time             `json:"scheduled_for,omitempty"`
	CheckedAt    time.Time              `json:"checked_at"`
}

// Validate는 공개 근거를 바꾸지 않고 상태별 필수·금지 시각과 확인 시각을 검사합니다.
// 이 검사는 durable cursor와 envelope가 같은 규칙으로 근거를 검사하도록 경계를 공유합니다.
func (p VideoPublicationV1) Validate(observedAt time.Time) error {
	if p.CheckedAt.IsZero() {
		return errors.New("publication checked_at is required")
	}

	if !observedAt.IsZero() && p.CheckedAt.After(observedAt) {
		return errors.New("publication checked_at is after the observation")
	}

	if optionalTimeIsZero(p.PublishedAt) || optionalTimeIsZero(p.ScheduledFor) {
		return errors.New("publication time must not be zero")
	}

	switch p.Status {
	case VideoPublicationPublished:
		if p.PublishedAt == nil || p.ScheduledFor != nil {
			return errors.New("published publication requires only published_at")
		}

		if p.PublishedAt.After(p.CheckedAt) {
			return errors.New("publication published_at is after its check")
		}
	case VideoPublicationUpcomingPremiere:
		if p.ScheduledFor == nil || p.PublishedAt != nil {
			return errors.New("upcoming premiere publication requires only scheduled_for")
		}
	case VideoPublicationUnresolved:
		if p.PublishedAt != nil || p.ScheduledFor != nil {
			return errors.New("unresolved publication must not carry times")
		}
	default:
		return fmt.Errorf("publication status %q is invalid", p.Status)
	}

	return nil
}

// normalizeVideoPublication은 공개 근거를 검사한 뒤 시각을 UTC로 정규화합니다.
func normalizeVideoPublication(p *VideoPublicationV1, observedAt time.Time) error {
	if err := p.Validate(observedAt); err != nil {
		return err
	}

	p.CheckedAt = p.CheckedAt.UTC()
	if err := normalizeOptionalTime(&p.PublishedAt); err != nil {
		return fmt.Errorf("publication published at: %w", err)
	}

	if err := normalizeOptionalTime(&p.ScheduledFor); err != nil {
		return fmt.Errorf("publication scheduled for: %w", err)
	}

	return nil
}

func validatePublicationItem(item *VideoListItemV1, observedAt time.Time) error {
	publication := item.Publication
	if publication == nil {
		if item.PublishedAt != nil || item.ScheduledFor != nil || item.IsPremiere != nil {
			return errors.New("video list item times require publication evidence")
		}

		return nil
	}

	if err := normalizeVideoPublication(publication, observedAt); err != nil {
		return fmt.Errorf("normalize and validate publication: %w", err)
	}

	premiere := publication.Status == VideoPublicationUpcomingPremiere
	if !sameOptionalTime(item.PublishedAt, publication.PublishedAt) || !sameOptionalTime(item.ScheduledFor, publication.ScheduledFor) {
		return errors.New("video list item times differ from publication evidence")
	}

	if premiere != (item.IsPremiere != nil && *item.IsPremiere) || (!premiere && item.IsPremiere != nil) {
		return errors.New("video list premiere flag differs from publication evidence")
	}

	return nil
}
