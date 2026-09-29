package internalhttp

import (
	"errors"
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

// CloseAll은 종료 경로에서 내부 client를 모두 닫는다. 하나가 실패해도 나머지를 닫고, 실패는 모아서 돌려준다.
// 각 Close는 nil receiver에서 안전해야 한다(설정하지 않은 선택 client는 typed nil로 들어온다).
func CloseAll(clients ...io.Closer) error {
	var errs []error

	for _, client := range clients {
		if client == nil {
			continue
		}

		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
