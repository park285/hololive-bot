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
