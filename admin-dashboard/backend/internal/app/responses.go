package app

import "github.com/kapu/admin-dashboard/internal/docker"

type statusResponse struct {
	Status string `json:"status"`
}

type loginResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	CSRFToken string `json:"csrf_token"`
}

type sessionPolicy struct {
	HeartbeatIntervalMS     uint64 `json:"heartbeat_interval_ms"`
	IdleTimeoutMS           uint64 `json:"idle_timeout_ms"`
	IdleWarningTimeoutMS    uint64 `json:"idle_warning_timeout_ms"`
	IdleSessionTTLMS        uint64 `json:"idle_session_ttl_ms"`
	AbsoluteWarningWindowMS uint64 `json:"absolute_warning_window_ms"`
}

type sessionStatusResponse struct {
	Status            string        `json:"status"`
	Authenticated     bool          `json:"authenticated"`
	Username          string        `json:"username"`
	AbsoluteExpiresAt int64         `json:"absolute_expires_at"`
	SessionPolicy     sessionPolicy `json:"session_policy"`
}

type heartbeatOKResponse struct {
	Status            string `json:"status"`
	AbsoluteExpiresAt int64  `json:"absolute_expires_at"`
}

type heartbeatIdleResponse struct {
	Status       string `json:"status"`
	IdleRejected bool   `json:"idle_rejected"`
}

type heartbeatRotatedResponse struct {
	Status            string `json:"status"`
	Rotated           bool   `json:"rotated"`
	AbsoluteExpiresAt int64  `json:"absolute_expires_at"`
	CSRFToken         string `json:"csrf_token"`
}

type dockerHealthResponse struct {
	Status    string `json:"status"`
	Available bool   `json:"available"`
}

type dockerContainersResponse struct {
	Status     string             `json:"status"`
	Containers []docker.Container `json:"containers"`
}

type dockerActionResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}
