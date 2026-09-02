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
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// wsAllowedOrigins: 설정(ServerConfig.WebSocketAllowedOrigins)이 넘긴 허용 오리진 목록.
var wsAllowedOrigins atomic.Pointer[[]string]

// InitWSUpgrader는 허용 오리진을 저장한다. 비어 있으면 모든 WebSocket 연결을 거부한다(secure default).
func InitWSUpgrader(origins []string) {
	copied := slices.Clone(origins)
	wsAllowedOrigins.Store(&copied)
}

func allowedWSOrigins() []string {
	if origins := wsAllowedOrigins.Load(); origins != nil {
		return *origins
	}

	return nil
}

// checkOrigin: 요청의 Origin 헤더가 허용 목록에 있는지 검증합니다.
// 허용 목록이 비어있으면 모든 연결을 거부합니다.
func checkOrigin(r *http.Request) bool {
	allowedOrigins := allowedWSOrigins()
	if len(allowedOrigins) == 0 {
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	for _, allowed := range allowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}

	return false
}

var WSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkOrigin,
}
