package httpx

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
)

type jsonResponse struct {
	data any
}

const maxRetainedJSONBuffer = 1 << 20

var responseBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// JSONWriter는 이미 검증한 비공개 데이터에서 올바른 UTF-8 JSON을 생성하는 응답 계약입니다.
// 구현은 HTML의 <, >, &를 이스케이프하고 I/O 실패를 반환해야 하며 renderer는 완료 전 본문을 보내지 않습니다.
type JSONWriter interface {
	WriteJSONTo(io.Writer) error
}

func (r jsonResponse) Render(w http.ResponseWriter) error {
	buffer, ok := responseBuffers.Get().(*bytes.Buffer)
	if !ok || buffer == nil {
		return errors.New("invalid response buffer pool entry")
	}

	buffer.Reset()

	defer func() {
		// 큰 단발 응답이 pool의 상주 메모리를 늘리지 않도록 보관 크기를 제한합니다.
		if buffer.Cap() <= maxRetainedJSONBuffer {
			buffer.Reset()
			responseBuffers.Put(buffer)
		}
	}()

	// 완성한 JSON만 기록하되 반복 응답의 출력 복사·버퍼 확장 할당은 재사용으로 줄입니다.
	if err := marshalJSONResponse(buffer, r.data); err != nil {
		return fmt.Errorf("marshal JSON response: %w", err)
	}

	r.WriteContentType(w)
	// 기존 raw 응답처럼 완성한 본문의 길이를 알려 불필요한 chunked 전송을 피합니다.
	w.Header().Set("Content-Length", strconv.Itoa(buffer.Len()))

	if _, err := w.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("write JSON response: %w", err)
	}

	return nil
}

func marshalJSONResponse(buffer *bytes.Buffer, data any) error {
	if response, ok := data.(JSONWriter); ok {
		if err := response.WriteJSONTo(buffer); err != nil {
			return fmt.Errorf("write validated JSON response: %w", err)
		}

		return nil
	}

	return jsonv2.MarshalWrite(buffer, data, jsontext.EscapeForHTML(true))
}

func (r jsonResponse) WriteContentType(w http.ResponseWriter) {
	if len(w.Header()["Content-Type"]) == 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
}

// Respond는 관리자 JSON을 UTF-8 검증·HTML 이스케이프 후 한 번에 기록합니다.
// 인코딩·쓰기 실패와 본문 없는 상태 코드는 Gin의 기존 render 수명으로 전달합니다.
func Respond(c *gin.Context, status int, data any) {
	c.Render(status, jsonResponse{data: data})
}
