// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
