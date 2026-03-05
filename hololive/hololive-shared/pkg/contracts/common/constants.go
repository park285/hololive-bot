package common

const (
	// APIKeyHeader: API 인증에 사용되는 HTTP 헤더 이름
	APIKeyHeader = "X-API-Key" //nolint:gosec // G101: 헤더 이름일 뿐 실제 credentials가 아님
)
