package sourceobservation

import (
	"errors"
	"fmt"
	"time"
)

const VideoListPublicationContractGeneration int64 = 2

type VideoPublicationStatus string

const (
	VideoPublicationPublished        VideoPublicationStatus = "PUBLISHED"
	VideoPublicationUpcomingPremiere VideoPublicationStatus = "UPCOMING_PREMIERE"
	VideoPublicationUnresolved       VideoPublicationStatus = "UNRESOLVED"
)

type VideoPublicationV1 struct {
	Status       VideoPublicationStatus `json:"status"`
	PublishedAt  *time.Time             `json:"published_at,omitempty"`
	ScheduledFor *time.Time             `json:"scheduled_for,omitempty"`
	CheckedAt    time.Time              `json:"checked_at"`
}

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
