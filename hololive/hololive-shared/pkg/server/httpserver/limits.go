package httpserver

import "time"

// 공개 런타임·metrics 서버가 공유하는 연결 제한입니다. 프로파일 스트리밍이 잘리지 않도록 pprof 서버는 헤더 제한만 씁니다.
const (
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 15 * time.Second
	serverWriteTimeout      = 60 * time.Second
	serverIdleTimeout       = 60 * time.Second
	serverMaxHeaderBytes    = 1 << 20 // 1MiB
)

// trustedProxies는 Cloudflare 형상에서 같은 호스트의 프록시만 X-Forwarded-For 출처로 신뢰하기 위한 목록입니다.
var trustedProxies = []string{"127.0.0.1", "::1"}
