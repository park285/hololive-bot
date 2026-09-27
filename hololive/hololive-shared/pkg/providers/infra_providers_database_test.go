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

package providers

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// DEC-20260926-stack-shared-go-compat-api-retirement: hololive는 적재한 POSTGRES_SSLROOTCERT 값을
// pgxdb Config.SSLRootCert로 직접 넘긴다. 이후 shared-go pgxdb의 env 폴백이 사라져도 CA 경로가 유지되어야 한다.
// DSN 해석 단계에서 pgx가 sslrootcert 파일을 읽으므로, 없는 경로를 주면 네트워크 연결 전에 그 경로로 실패한다.
func TestProvideDatabaseResourcesPassesSSLRootCertExplicitly(t *testing.T) {
	t.Setenv("POSTGRES_SSLROOTCERT", "")

	missingCA := filepath.Join(t.TempDir(), "missing-postgres-ca.pem")
	config := &settings.PostgresConfig{
		Host:         "127.0.0.1",
		Port:         1,
		User:         "hololive_test",
		Password:     "test-password",
		Database:     "hololive_test",
		SSLMode:      "verify-full",
		SSLRootCert:  missingCA,
		PoolMinConns: 1,
		PoolMaxConns: 1,
	}

	resources, cleanup, err := ProvideDatabaseResources(t.Context(), config, slog.New(slog.DiscardHandler))
	if err == nil {
		cleanup()
		t.Fatalf("ProvideDatabaseResources() = %v, want CA file read failure", resources)
	}

	if !strings.Contains(err.Error(), missingCA) {
		t.Fatalf("ProvideDatabaseResources() error = %v, want failure naming sslrootcert %q", err, missingCA)
	}
}
