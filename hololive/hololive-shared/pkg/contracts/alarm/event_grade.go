package alarm

import "time"

const EventGradePath = BasePath + "/event-grade"

type GradeRegion string

func (r GradeRegion) Valid() bool {
	return r == "jp" || r == "us" || r == "as"
}

type EventGradeRequest struct {
	RoomID string      `json:"room_id"`
	UserID string      `json:"user_id"`
	Region GradeRegion `json:"region"`
	Score  int64       `json:"score"`
}

func (r EventGradeRequest) Valid() bool {
	return r.Region.Valid() && r.Score > 0 && validGradeIdentity(r.RoomID) && validGradeIdentity(r.UserID)
}

func validGradeIdentity(s string) bool {
	if len(s) < 1 || len(s) > 100 {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

type EventGradeResponse struct {
	Status    string      `json:"status"`
	Region    GradeRegion `json:"region"`
	EventType string      `json:"event_type,omitempty"`
	EndsAt    time.Time   `json:"ends_at,omitzero"`
	RevealsAt time.Time   `json:"reveals_at,omitzero"`
}

func (r EventGradeResponse) Valid(region GradeRegion) bool {
	if !region.Valid() || r.Region != region {
		return false
	}

	if r.Status == "unavailable" {
		return r.EventType == "" && r.EndsAt.IsZero() && r.RevealsAt.IsZero()
	}

	return r.Status == "registered" && (r.EventType == "normal" || r.EventType == "relay") && !r.EndsAt.IsZero() && !r.RevealsAt.Before(r.EndsAt)
}
