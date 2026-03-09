// Package server: HTTP 서버 요청/응답 타입 정의
package server

// ===== Common Types =====

// ErrorResponse: 공통 에러 응답
type ErrorResponse struct {
	Error   string `json:"error" example:"Unauthorized"`
	Details string `json:"details,omitempty" example:"Session expired"`
}

// StatusResponse: 공통 상태 응답
type StatusResponse struct {
	Status  string `json:"status" example:"ok"`
	Message string `json:"message,omitempty" example:"Operation successful"`
}

// ===== Auth Types =====

// LoginRequest: 로그인 요청
type LoginRequest struct {
	Username string `json:"username" binding:"required" example:"admin"`
	Password string `json:"password" binding:"required" example:"password123"`
}

// LoginResponse: 로그인 응답
type LoginResponse struct {
	Status  string `json:"status" example:"ok"`
	Message string `json:"message" example:"Login successful"`
}

// HeartbeatRequest: 하트비트 요청
type HeartbeatRequest struct {
	Idle bool `json:"idle" example:"false"`
}

// HeartbeatResponse: 하트비트 응답
type HeartbeatResponse struct {
	Status            string `json:"status" example:"ok"`
	Rotated           bool   `json:"rotated,omitempty" example:"true"`
	AbsoluteExpiresAt int64  `json:"absolute_expires_at,omitempty" example:"1704067200"`
	IdleRejected      bool   `json:"idle_rejected,omitempty" example:"false"`
}

// ===== Docker Types =====

// DockerHealthResponse: Docker 헬스 응답
type DockerHealthResponse struct {
	Status    string `json:"status" example:"ok"`
	Available bool   `json:"available" example:"true"`
}

// ContainerInfo: 컨테이너 정보 (docker.Service에서 사용)
// 참조: internal/docker/docker.go
type ContainerInfo struct {
	Name           string  `json:"name" example:"hololive-bot"`
	ID             string  `json:"id" example:"abc123def456"`
	State          string  `json:"state" example:"running"`
	Status         string  `json:"status" example:"Up 2 hours"`
	CPUPercent     float64 `json:"cpuPercent" example:"1.5"`
	MemoryUsageMB  float64 `json:"memoryUsageMB" example:"128.5"`
	MemoryLimitMB  float64 `json:"memoryLimitMB" example:"512.0"`
	MemoryPercent  float64 `json:"memoryPercent" example:"25.1"`
	NetworkRxMB    float64 `json:"networkRxMB" example:"10.5"`
	NetworkTxMB    float64 `json:"networkTxMB" example:"5.2"`
	BlockReadMB    float64 `json:"blockReadMB" example:"100.0"`
	BlockWriteMB   float64 `json:"blockWriteMB" example:"50.0"`
	GoroutineCount int     `json:"goroutineCount,omitempty" example:"25"`
}

// ContainerListResponse: 컨테이너 목록 응답
type ContainerListResponse struct {
	Status     string          `json:"status" example:"ok"`
	Containers []ContainerInfo `json:"containers"`
}

// ===== Logs Types =====

// LogFile: 로그 파일 정보
type LogFile struct {
	Key         string `json:"key" example:"combined"`
	Name        string `json:"name" example:"combined.log"`
	Description string `json:"description" example:"All services combined log"`
}

// LogFilesResponse: 로그 파일 목록 응답
type LogFilesResponse struct {
	Status string    `json:"status" example:"ok"`
	Files  []LogFile `json:"files"`
}

// SystemLogsResponse: 시스템 로그 응답
type SystemLogsResponse struct {
	Status string   `json:"status" example:"ok"`
	File   string   `json:"file" example:"combined"`
	Lines  []string `json:"lines"`
	Count  int      `json:"count" example:"100"`
}
