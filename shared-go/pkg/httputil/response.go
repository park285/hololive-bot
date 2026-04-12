package httputil

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func CheckStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	const maxBodyLen = 4096
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
	if err != nil {
		return fmt.Errorf("status %d: read body: %w", resp.StatusCode, err)
	}
	return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func DecodeJSON(resp *http.Response, v any) error {
	defer func() { _ = resp.Body.Close() }()
	//nolint:wrapcheck // 호출부에서 컨텍스트 추가
	return json.NewDecoder(resp.Body).Decode(v)
}
