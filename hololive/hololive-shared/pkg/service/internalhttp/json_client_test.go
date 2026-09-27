package internalhttp

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewClientForURLStrictReturnsErrorWhenH3ClientConfigFails(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))

	client, err := NewClientForURLStrict("https://hololive-admin-api:30006", time.Second, nil)
	if err == nil {
		t.Fatal("NewClientForURLStrict() error = nil, want h3 client config error")
	}

	if client != nil {
		t.Fatalf("NewClientForURLStrict() client = %T, want nil on error", client)
	}
}

func TestNewClientForURLStrictKeepsPlainHTTPClient(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))

	client, err := NewClientForURLStrict("http://localhost:30190", time.Second, nil)
	if err != nil {
		t.Fatalf("NewClientForURLStrict(http) error = %v", err)
	}

	if client == nil {
		t.Fatal("NewClientForURLStrict(http) client = nil")
	}
}

// https 내부 URL의 JSON client는 HOLOLIVE_INTERNAL_H3_* 누락을 오류로 돌려준다. 경고 뒤 TCP client로 내려가면
// 기동은 성공하고 H3 전용 내부 서버 요청만 런타임에 실패하므로 그 폴백은 두지 않는다(stack audit 2026-09-26).
func TestNewJSONClientRequiresInternalH3EnvForHTTPS(t *testing.T) {
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", "")
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "")

	if client, err := NewJSONClient("https://127.0.0.1:30003", "key", time.Second); err == nil || client != nil {
		t.Fatalf("NewJSONClient(https) = (%v, %v), want missing internal H3 env error", client, err)
	}

	if client, err := NewJSONClient("http://127.0.0.1:30003", "key", time.Second); err != nil || client == nil {
		t.Fatalf("NewJSONClient(http) = (%v, %v), want plain internal client", client, err)
	}

	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", writeTestCACertificate(t))
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "127.0.0.1")

	client, err := NewJSONClient("https://127.0.0.1:30003", "key", time.Second)
	if err != nil || client == nil {
		t.Fatalf("NewJSONClient(https) = (%v, %v), want configured H3 client", client, err)
	}
}
