package settings

import (
	"os"
	"strings"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
)

// LoadInternalH3ClientOptions는 내부 서비스 H3 client의 TLS 경로와 이름을 전용 키 두 개에서만 읽는다.
// 공통 서버 키(HOLOLIVE_H3_CERT_FILE, HOLOLIVE_H3_SERVER_NAME)로 대신하지 않는다.
// 빈 값의 HTTPS 거절과 파일 검증은 client 생성자가 소유하여 HTTP-only runtime 기동을 보존한다.
func LoadInternalH3ClientOptions() sharedh3.ClientOptions {
	return sharedh3.ClientOptions{
		CACertFile: strings.TrimSpace(os.Getenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE")),
		ServerName: strings.TrimSpace(os.Getenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME")),
	}
}
