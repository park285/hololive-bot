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

package database

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// 설정의 CA 경로가 pgxdb.Config.SSLRootCert로 전달되는지 확인한다.
// 없는 CA 파일은 pgx의 DSN 해석 단계에서 네트워크 연결 전에 실패한다.
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
