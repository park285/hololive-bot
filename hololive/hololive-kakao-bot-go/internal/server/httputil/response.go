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

package httputil

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/ginjson"
)

// Success: 성공 응답을 작성합니다.
func Success(c *gin.Context, data any) {
	ginjson.Respond(c, http.StatusOK, data)
}

// SuccessWithStatus: 지정된 상태 코드로 성공 응답을 작성합니다.
func SuccessWithStatus(c *gin.Context, status int, data any) {
	ginjson.Respond(c, status, data)
}

// Error: 에러 응답을 작성합니다.
func Error(c *gin.Context, status int, message string) {
	ginjson.Respond(c, status, gin.H{"error": message})
}

// ErrorWithData: 추가 데이터가 포함된 에러 응답을 작성합니다.
func ErrorWithData(c *gin.Context, status int, data any) {
	ginjson.Respond(c, status, data)
}
