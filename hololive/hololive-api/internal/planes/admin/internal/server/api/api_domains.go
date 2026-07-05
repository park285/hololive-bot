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

package api

// route group별로 API 책임을 나눈 domain handler들이다.
type (
	MemberHandler      struct{ *Handler }
	AlarmHandler       struct{ *Handler }
	RoomHandler        struct{ *Handler }
	StreamHandler      struct{ *Handler }
	StatsHandler       struct{ *Handler }
	SettingsAPIHandler struct{ *Handler }
	TemplateHandler    struct{ *Handler }
	ProfileHandler     struct{ *Handler }
	MajorEventHandler  struct{ *Handler }
	OAuthHandler       struct{ *Handler }
)

type DomainHandlers struct {
	Member     *MemberHandler
	Alarm      *AlarmHandler
	Room       *RoomHandler
	Stream     *StreamHandler
	Stats      *StatsHandler
	Settings   *SettingsAPIHandler
	Template   *TemplateHandler
	Profile    *ProfileHandler
	MajorEvent *MajorEventHandler
	OAuth      *OAuthHandler
}

func (h *Handler) DomainHandlers() *DomainHandlers {
	h = h.ensureDefaults()

	return &DomainHandlers{
		Member:     &MemberHandler{Handler: h},
		Alarm:      &AlarmHandler{Handler: h},
		Room:       &RoomHandler{Handler: h},
		Stream:     &StreamHandler{Handler: h},
		Stats:      &StatsHandler{Handler: h},
		Settings:   &SettingsAPIHandler{Handler: h},
		Template:   &TemplateHandler{Handler: h},
		Profile:    &ProfileHandler{Handler: h},
		MajorEvent: &MajorEventHandler{Handler: h},
		OAuth:      &OAuthHandler{Handler: h},
	}
}
