package internalhttp

import (
	"fmt"
	"io"
	"net/http"
)

// H3 client의 QUIC 연결은 서버 keep-alive 때문에 idle timeout으로 회수되지 않으므로,
// 소유자가 종료 시점에 명시적으로 닫아야 프로세스가 그 연결을 들고 죽지 않는다.
func CloseClient(client *http.Client) error {
	if client == nil {
		return nil
	}

	closer, ok := client.Transport.(io.Closer)
	if !ok {
		return nil
	}

	if err := closer.Close(); err != nil {
		return fmt.Errorf("close internal http transport: %w", err)
	}

	return nil
}
