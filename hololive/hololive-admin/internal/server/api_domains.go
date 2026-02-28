package server

// API domain handlers split API responsibilities by route group.
type MemberAPIHandler struct{ *APIHandler }
type AlarmAPIHandler struct{ *APIHandler }
type RoomAPIHandler struct{ *APIHandler }
type StreamAPIHandler struct{ *APIHandler }
type StatsAPIHandler struct{ *APIHandler }
type SettingsAPIHandler struct{ *APIHandler }
type TemplateAPIHandler struct{ *APIHandler }
type MilestoneAPIHandler struct{ *APIHandler }
type ProfileAPIHandler struct{ *APIHandler }
type MajorEventAPIHandler struct{ *APIHandler }
type OAuthAPIHandler struct{ *APIHandler }

// DomainAPIHandlers groups domain-scoped API handlers for route registration.
type DomainAPIHandlers struct {
	Member     *MemberAPIHandler
	Alarm      *AlarmAPIHandler
	Room       *RoomAPIHandler
	Stream     *StreamAPIHandler
	Stats      *StatsAPIHandler
	Settings   *SettingsAPIHandler
	Template   *TemplateAPIHandler
	Milestone  *MilestoneAPIHandler
	Profile    *ProfileAPIHandler
	MajorEvent *MajorEventAPIHandler
	OAuth      *OAuthAPIHandler
}

func (h *APIHandler) DomainHandlers() *DomainAPIHandlers {
	if h == nil {
		h = &APIHandler{}
	}

	return &DomainAPIHandlers{
		Member:     &MemberAPIHandler{APIHandler: h},
		Alarm:      &AlarmAPIHandler{APIHandler: h},
		Room:       &RoomAPIHandler{APIHandler: h},
		Stream:     &StreamAPIHandler{APIHandler: h},
		Stats:      &StatsAPIHandler{APIHandler: h},
		Settings:   &SettingsAPIHandler{APIHandler: h},
		Template:   &TemplateAPIHandler{APIHandler: h},
		Milestone:  &MilestoneAPIHandler{APIHandler: h},
		Profile:    &ProfileAPIHandler{APIHandler: h},
		MajorEvent: &MajorEventAPIHandler{APIHandler: h},
		OAuth:      &OAuthAPIHandler{APIHandler: h},
	}
}
