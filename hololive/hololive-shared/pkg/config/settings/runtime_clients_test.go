package settings

import "testing"

// 내부 client는 서버용 키를 대신 쓰지 않고 전용 설정만 적재한다. HTTPS 필수값 검증은 client 생성 시점에 한다.
func TestLoadInternalH3ClientOptionsKeepsDedicatedStartupValues(t *testing.T) {
	t.Setenv("HOLOLIVE_H3_CERT_FILE", "/server-only.crt")
	t.Setenv("HOLOLIVE_H3_SERVER_NAME", "server-only.example")
	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", "")
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "")

	blank := LoadInternalH3ClientOptions()
	if blank.CACertFile != "" || blank.ServerName != "" {
		t.Fatalf("blank internal H3 settings = %+v, want no server-key substitution", blank)
	}

	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", " /internal-ca.crt ")
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", " internal.example ")

	loaded := LoadInternalH3ClientOptions()
	if loaded.CACertFile != "/internal-ca.crt" || loaded.ServerName != "internal.example" {
		t.Fatalf("internal H3 settings = %+v, want trimmed dedicated values", loaded)
	}
}
