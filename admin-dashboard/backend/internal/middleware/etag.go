// Package middleware: HTTP 미들웨어
package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// etagWriter: 응답 본문을 캡처하여 ETag 계산
type etagWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *etagWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	n, err := w.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write response: %w", err)
	}
	return n, nil
}

// ETag: GET 요청에 대해 ETag 헤더 추가 및 조건부 요청 처리
// - 응답 본문의 SHA256 해시를 ETag로 사용
// - If-None-Match 헤더와 일치하면 304 Not Modified 반환
func ETag() gin.HandlerFunc {
	return func(c *gin.Context) {
		// GET 요청만 처리
		if c.Request.Method != http.MethodGet {
			c.Next()
			return
		}

		// API 경로만 처리 (정적 자산 제외)
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/admin/api/") {
			c.Next()
			return
		}

		// WebSocket 제외
		if c.GetHeader("Upgrade") == "websocket" {
			c.Next()
			return
		}

		// 응답 캡처
		buf := new(bytes.Buffer)
		writer := &etagWriter{
			ResponseWriter: c.Writer,
			body:           buf,
		}
		c.Writer = writer

		c.Next()

		// 200 OK 응답만 ETag 적용
		if c.Writer.Status() != http.StatusOK {
			return
		}

		// 본문이 없으면 스킵
		if buf.Len() == 0 {
			return
		}

		// ETag 생성 (SHA256 해시, 앞 16자)
		hash := sha256.Sum256(buf.Bytes())
		etag := `"` + hex.EncodeToString(hash[:8]) + `"`

		// If-None-Match 확인
		ifNoneMatch := c.GetHeader("If-None-Match")
		if ifNoneMatch != "" && ifNoneMatch == etag {
			// 304 Not Modified
			c.Writer = writer.ResponseWriter // 원래 writer 복원
			c.Status(http.StatusNotModified)
			return
		}

		// ETag 헤더 설정
		c.Header("ETag", etag)
		// 프록시/CDN 캐시 허용
		if c.Writer.Header().Get("Cache-Control") == "" {
			c.Header("Cache-Control", "private, max-age=0, must-revalidate")
		}
	}
}
