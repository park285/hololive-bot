package dbx

import (
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsDuplicateKey: duplicate key 에러인지 확인 (INSERT 시 unique constraint 위반)
func IsDuplicateKey(err error) bool {
	return hasPGCode(err, pgerrcode.UniqueViolation)
}

// IsForeignKeyViolation: foreign key 제약 위반인지 확인
func IsForeignKeyViolation(err error) bool {
	return hasPGCode(err, pgerrcode.ForeignKeyViolation)
}

// IsCheckViolation: check 제약 위반인지 확인
func IsCheckViolation(err error) bool {
	return hasPGCode(err, pgerrcode.CheckViolation)
}

// IsNotNullViolation: not null 제약 위반인지 확인
func IsNotNullViolation(err error) bool {
	return hasPGCode(err, pgerrcode.NotNullViolation)
}

// hasPGCode: PostgreSQL 에러 코드 확인
func hasPGCode(err error, code string) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == code
	}
	return false
}

// PGError: PostgreSQL 에러 정보 추출
// 에러가 *pgconn.PgError가 아니면 nil 반환
func PGError(err error) *pgconn.PgError {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr
	}
	return nil
}

// IsDNSError: DNS 조회 에러인지 확인
func IsDNSError(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "no such host")
}

// ShouldFallbackToLocalhost: DNS 에러 시 127.0.0.1 fallback 필요 여부 판단
// host가 "postgres"이고 DNS 에러인 경우에만 true 반환 (Docker 환경에서 로컬 실행 시)
func ShouldFallbackToLocalhost(err error, host string) bool {
	if err == nil {
		return false
	}
	if host == "" || host == "127.0.0.1" || strings.EqualFold(host, "localhost") {
		return false
	}
	if !strings.EqualFold(host, "postgres") {
		return false
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return strings.EqualFold(dnsErr.Name, host)
	}

	lower := strings.ToLower(err.Error())
	hostLower := strings.ToLower(host)
	if strings.Contains(lower, "lookup "+hostLower) && strings.Contains(lower, "no such host") {
		return true
	}
	return strings.Contains(lower, "no such host") && strings.Contains(lower, hostLower)
}
