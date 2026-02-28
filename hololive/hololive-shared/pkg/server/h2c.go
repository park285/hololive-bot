package server

import (
	"net/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// WrapH2C: HTTP/2 Cleartext 지원을 위해 핸들러를 래핑한다.
// TLS 없이 HTTP/2 프로토콜을 사용하여 멀티플렉싱과 헤더 압축 이점을 제공합니다.
func WrapH2C(handler http.Handler) http.Handler {
	return h2c.NewHandler(handler, &http2.Server{})
}
