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

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/httputil/ginauth"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

const (
	// APIKeyHeader: 하위 호환성을 위한 재수출. 실제 정의는 contracts/common 패키지에 있습니다.
	APIKeyHeader = common.APIKeyHeader
)

func errorPayload(code, message string) gin.H {
	payload := gin.H{"error": code}
	if message != "" {
		payload["message"] = message
	}
	return payload
}

func abortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorPayload(code, message))
}

func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	return ginauth.APIKeyAuthMiddleware(apiKey)
}

func NoRouteAuthHandler(apiKey string) gin.HandlerFunc {
	return ginauth.NoRouteAuthHandler(apiKey)
}
