package observations

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"github.com/park285/shared-go/v2/pkg/panicguard"
)

const (
	wsSessionRevocationPoll = time.Second
	wsWriteWait             = 5 * time.Second
)

func (s *Streams) streamSystemStats(ctx context.Context, conn *websocket.Conn, familyID string) {
	history, updates, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	peerGone := watchPeer(conn, s.timing.PongWait)

	defer func() { closeConn(conn); <-peerGone }()

	stopRevocationWatch := s.watchSessionFamilyRevocation(ctx, conn, familyID)

	defer stopRevocationWatch()

	for _, stats := range history {
		if !writeSystemStatsFrame(conn, stats) {
			return
		}
	}

	s.pumpSystemStats(conn, updates, peerGone)
}

func (s *Streams) watchSessionFamilyRevocation(parent context.Context, conn *websocket.Conn, familyID string) context.CancelFunc {
	ctx, cancel := context.WithCancel(parent)

	done := make(chan struct{})

	go func() {
		defer close(done)

		panicguard.Run(s.logger, panicguard.BackgroundTask, "admin-dashboard-websocket-session-revocation", func() { s.pollSessionFamilyRevocation(ctx, conn, familyID) })
	}()

	return func() { cancel(); <-done }
}

func (s *Streams) pollSessionFamilyRevocation(
	ctx context.Context,
	conn *websocket.Conn,
	familyID string,
) {
	ticker := time.NewTicker(wsSessionRevocationPoll)
	defer ticker.Stop()

	for awaitRevocationTick(ctx, ticker) {
		if !s.sessionFamilyStillActive(ctx, conn, familyID) {
			return
		}
	}
}

func awaitRevocationTick(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticker.C:
		return true
	}
}

func (s *Streams) sessionFamilyStillActive(
	ctx context.Context,
	conn *websocket.Conn,
	familyID string,
) bool {
	checkCtx, checkCancel := context.WithTimeout(ctx, wsSessionRevocationPoll)
	defer checkCancel()

	active, err := s.policy.FamilyActive(checkCtx, familyID)
	if err != nil {
		s.logger.Warn("websocket session-family check failed; closing stream", slog.Any("error", err))
		s.closeWebSocketForRevocation(conn, "session store unavailable")

		return false
	}

	if !active {
		s.closeWebSocketForRevocation(conn, "session revoked")

		return false
	}

	return true
}

func (s *Streams) closeWebSocketForRevocation(conn *websocket.Conn, reason string) {
	deadline := time.Now().Add(wsWriteWait)
	if err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason), deadline); err != nil {
		s.logger.Debug("write websocket revocation close frame", slog.Any("error", err))
	}

	if err := conn.Close(); err != nil {
		s.logger.Debug("close revoked websocket", slog.Any("error", err))
	}
}

func (s *Streams) pumpSystemStats(conn *websocket.Conn, updates <-chan SystemStats, peerGone <-chan struct{}) {
	ping := time.NewTicker(s.timing.PingPeriod)
	defer ping.Stop()

	for {
		if !pumpSystemStatsOnce(conn, updates, peerGone, ping.C) {
			return
		}
	}
}

func pumpSystemStatsOnce(conn *websocket.Conn, updates <-chan SystemStats, peerGone <-chan struct{}, tick <-chan time.Time) bool {
	select {
	case <-peerGone:
		return false
	case <-tick:
		return writePing(conn)
	case stats, ok := <-updates:
		return ok && writeSystemStatsFrame(conn, stats)
	}
}

func watchPeer(conn *websocket.Conn, pongWait time.Duration) <-chan struct{} {
	gone := make(chan struct{})

	conn.SetReadLimit(512)

	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		close(gone)

		return gone
	}

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	go panicguard.Run(nil, panicguard.BackgroundTask, "admin-dashboard-websocket-peer", func() {
		defer close(gone)

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	return gone
}

func writePing(conn *websocket.Conn) bool {
	if err := conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return false
	}

	return conn.WriteMessage(websocket.PingMessage, nil) == nil
}

func writeSystemStatsFrame(conn *websocket.Conn, stats any) bool {
	if err := setWriteDeadline(conn); err != nil {
		return false
	}

	payload, err := jsonv2.Marshal(stats)
	if err != nil {
		return false
	}

	return conn.WriteMessage(websocket.TextMessage, payload) == nil
}

func closeConn(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		return
	}
}

func setWriteDeadline(conn *websocket.Conn) error {
	if err := conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}

	return nil
}
