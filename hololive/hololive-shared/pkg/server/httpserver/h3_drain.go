package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/quic-go/quic-go/http3"
)

// h3RequestDrain은 HTTP/3 handler에서 실행 중인 요청 수를 센다.
//
// 세기 시작하는 지점은 handler 진입이다. 지금 쓰는 quic-go v0.63의 http3.Server는 요청 stream을 자기 accept
// loop에서 받아 곧바로 HEADERS를 파싱하고, 그 사이에 끼어들 hook이 없다. ConnContext는 연결 단위이고 *quic.Conn은
// 열린 stream을 세는 API가 없으며, v0.63 http3에는 StreamHijacker가 없다. NewRawServerConn으로 accept loop를 직접
// 돌리면 GOAWAY를 보내는 control stream에 접근하지 못한다. 그래서 stream은 받았지만 HEADERS가 덜 도착한 요청은
// 세지 않는다.
//
// 이 틈이 판정을 바꾸는 방향은 한쪽뿐이다. 시한에 그런 stream만 남아 있으면 shutdownH3Server는 성공(nil)을
// 돌려준다. 세었다면 ctx 오류였을 경우다. 반대로 실패를 만들지는 않는다. 요청이 끊기는 결과는 두 판정에서 같다.
// 시한이 되면 어느 쪽이든 abort가 연결을 닫고, 그 요청은 handler에 닿지 않았으므로 서버 쪽 부수효과가 없으며
// client는 연결 종료 오류를 받는다. 이 상태가 되려면 GOAWAY 전에 받은 stream의 HEADERS 나머지가 종료 시한 내내
// 도착하지 않아야 한다. GOAWAY 뒤의 새 stream은 quic-go가 H3_REQUEST_REJECTED로 거절하고, 서버는 첫 STREAM frame이
// 와야 stream을 받는다.
type h3RequestDrain struct {
	active atomic.Int64
}

func (d *h3RequestDrain) wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.active.Add(1)
		defer d.active.Add(-1)

		next.ServeHTTP(w, r)
	})
}

// drained는 실행 중인 요청이 없음을 셀 수 있을 때만 true다. NewRuntimeHTTPServers 밖에서 만든 서버(nil drain)는
// 요청을 세지 않으므로 drain을 증명하지 못한다.
func (d *h3RequestDrain) drained() bool {
	return d != nil && d.active.Load() == 0
}

// shutdownH3Server는 quic-go Shutdown으로 GOAWAY를 보내고 client가 연결을 닫기를 ctx 안에서 기다린다.
// 요청이 끝나도 quic-go는 client가 닫지 않은 연결을 기다린다. 같은 compose stop에서 먼저 끝난 client(v7.0.0 cutover의
// hololive-api)는 CONNECTION_CLOSE 없이 사라져 그 연결이 QUIC idle timeout(60s)까지 남고, 종료 시한(10s)을 넘겨
// 정상 정지가 exit 1이 됐다. 시한이 끝난 순간 실행 중인 요청이 없으면 남은 것은 idle 연결뿐이므로 닫고 성공한다.
// 그때 요청이 실행 중이면 그 요청을 끊으므로 ctx 오류를 종료 실패로 돌려준다. HEADERS를 파싱 중인 요청은 세지
// 않는다(h3RequestDrain 참고).
func shutdownH3Server(ctx context.Context, server *http3.Server, drain *h3RequestDrain) error {
	if server == nil {
		return nil
	}

	// quic-go는 자기 ctx가 끝나면 Close로 연결과 요청을 끊는다. 끊기 전에 실행 중인 요청을 세도록 ctx를 직접 끝낸다.
	shutdownCtx, abort := context.WithCancel(context.WithoutCancel(ctx))
	defer abort()

	done := make(chan error, 1)

	go func() {
		done <- server.Shutdown(shutdownCtx)
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP/3 server shutdown failed: %w", err)
		}

		return nil
	case <-ctx.Done():
		idleOnly := drain.drained()

		abort()
		<-done

		if idleOnly {
			return nil
		}

		return fmt.Errorf("HTTP/3 server shutdown failed: %w", ctx.Err())
	}
}
