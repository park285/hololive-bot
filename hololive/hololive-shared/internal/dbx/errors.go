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

package dbx

import (
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if hasPGCode(err, pgerrcode.UniqueViolation) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value violates unique constraint")
}

func IsForeignKeyViolation(err error) bool {
	return hasPGCode(err, pgerrcode.ForeignKeyViolation)
}

func IsCheckViolation(err error) bool {
	return hasPGCode(err, pgerrcode.CheckViolation)
}

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

// host가 "postgres"이고 DNS 에러인 경우에만 true 반환 (Docker 환경에서 로컬 실행 시)
func ShouldFallbackToLocalhost(err error, host string) bool {
	if err == nil {
		return false
	}
	if !isFallbackEligibleHost(host) {
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

func isFallbackEligibleHost(host string) bool {
	if host == "" || host == "127.0.0.1" || strings.EqualFold(host, "localhost") {
		return false
	}
	return strings.EqualFold(host, "postgres")
}
