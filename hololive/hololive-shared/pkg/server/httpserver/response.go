// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package httpserver

import (
	"log/slog"
	"maps"

	"github.com/gin-gonic/gin"
)

// RespondError는 API 에러 응답 payload를 일관된 형식으로 반환합니다.
func RespondError(c *gin.Context, status int, message string, extra gin.H) {
	payload := gin.H{"error": message}
	maps.Copy(payload, extra)
	c.JSON(status, payload)
}

// RespondInternalError는 내부 에러를 로그에 남기고 500 에러 응답을 반환합니다.
func RespondInternalError(
	logger *slog.Logger,
	c *gin.Context,
	userMessage,
	logMessage string,
	err error,
	attrs ...slog.Attr,
) {
	if logger != nil {
		logAttrs := make([]any, 0, len(attrs)+1)
		logAttrs = append(logAttrs, slog.Any("error", err))
		for _, attr := range attrs {
			logAttrs = append(logAttrs, attr)
		}
		logger.Error(logMessage, logAttrs...)
	}

	RespondError(c, 500, userMessage, nil)
}
