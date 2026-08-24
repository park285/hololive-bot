package domain

import (
	"errors"
	"fmt"
	"strings"
)

func (e *AlarmQueueEnvelope) validateCelebrationDispatch() error {
	if e.Notification.RoomID == "" {
		return errors.New("canonical alarm dispatch: celebration room id is empty")
	}

	at := e.Notification.AlarmType
	if at != AlarmTypeBirthday && at != AlarmTypeAnniversary {
		return fmt.Errorf("canonical alarm dispatch: celebration alarm type %q is not birthday or anniversary", at)
	}

	if e.Celebration == nil {
		return errors.New("canonical alarm dispatch: celebration payload is nil")
	}

	if e.Celebration.Date == "" {
		return errors.New("canonical alarm dispatch: celebration date is empty")
	}

	if e.Celebration.Kind == CelebrationKindBirthdayStream && strings.TrimSpace(e.Celebration.VideoID) == "" {
		return errors.New("canonical alarm dispatch: celebration birthday stream video id is empty")
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateYouTubeOutboxDispatch() error {
	if err := validateCanonicalNotification(&e.Notification); err != nil {
		return fmt.Errorf("validate canonical notification: %w", err)
	}

	if e.YouTubeOutbox == nil {
		return errors.New("canonical alarm dispatch: youtube outbox payload is nil")
	}

	if err := e.YouTubeOutbox.Validate(); err != nil {
		return fmt.Errorf("canonical alarm dispatch: %w", err)
	}

	if err := validateCanonicalYouTubeOutboxMatch(&e.Notification, e.YouTubeOutbox); err != nil {
		return fmt.Errorf("validate canonical youtube outbox match: %w", err)
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateDeliveryDigestDispatch() error {
	if err := validateCanonicalNotification(&e.Notification); err != nil {
		return fmt.Errorf("validate canonical notification: %w", err)
	}

	if e.Notification.AlarmType != AlarmTypeCommunity {
		return fmt.Errorf("canonical alarm dispatch: delivery digest storage alarm type %q is not community", e.Notification.AlarmType)
	}

	if e.DeliveryDigest == nil {
		return errors.New("canonical alarm dispatch: delivery digest payload is nil")
	}

	if err := e.DeliveryDigest.Validate(); err != nil {
		return fmt.Errorf("canonical alarm dispatch: %w", err)
	}

	return nil
}

func validateCanonicalNotification(notification *AlarmNotification) error {
	if notification == nil {
		return errors.New("canonical alarm dispatch: notification is nil")
	}

	switch {
	case notification.RoomID == "":
		return errors.New("canonical alarm dispatch: room id is empty")
	case notification.AlarmType == "":
		return errors.New("canonical alarm dispatch: alarm type is empty")
	case !notification.AlarmType.IsValid():
		return fmt.Errorf("canonical alarm dispatch: alarm type %q is invalid", notification.AlarmType)
	default:
		return nil
	}
}

func validateCanonicalYouTubeOutboxMatch(notification *AlarmNotification, payload *YouTubeOutboxDispatchPayload) error {
	if notification.AlarmType != payload.AlarmType {
		return fmt.Errorf("canonical alarm dispatch: notification alarm type %q does not match source alarm type %q", notification.AlarmType, payload.AlarmType)
	}

	return nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}
