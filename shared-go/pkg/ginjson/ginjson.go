package ginjson

import (
	"net/http"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
)

type JSON struct {
	Data any
}

var jsonContentType = []string{"application/json; charset=utf-8"}

func (r JSON) Render(w http.ResponseWriter) error {
	r.WriteContentType(w)
	// Sonic의 Encoder 인터페이스는 표준 라이브러리의 Encode 메서드와 호환됨
	enc := sonic.ConfigDefault.NewEncoder(w)
	enc.SetEscapeHTML(true)
	return enc.Encode(r.Data)
}

func (r JSON) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = jsonContentType
	}
}

func Respond(c *gin.Context, status int, data any) {
	c.Render(status, JSON{Data: data})
}
