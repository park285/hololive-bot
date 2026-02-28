package server

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

var allowedOrigins = []string{
	"https://bot.example.com",
	"https://admin.example.com",
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}

		for _, allowed := range allowedOrigins {
			if strings.EqualFold(origin, allowed) {
				return true
			}
		}

		host := r.Host
		if strings.HasPrefix(origin, "http://"+host) || strings.HasPrefix(origin, "https://"+host) {
			return true
		}

		return false
	},
}

// WSUpgrader: API WebSocket 업그레이더 기본 설정입니다.
var WSUpgrader = wsUpgrader
