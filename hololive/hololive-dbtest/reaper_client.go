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

package dbtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// upstream reaper 연결은 TCP dial 직후 성공을 반환하고 filter 전송·ACK 수신은
// goroutine에서 실행돼 실패해도 로그만 남는다. 프로비저닝 flock 안에서 동기
// handshake를 마친 연결을 프로세스 수명 동안 보유해 바이너리마다 검증된 client를 보장한다.
var verifiedReaperConn net.Conn

const (
	verifyReaperTimeout = 10 * time.Second
	reaperRetryInterval = 100 * time.Millisecond
	reaperAck           = "ACK\n"
	reaperVersionLabel  = "org.testcontainers.version"
)

var (
	errSessionReaperUnavailable = errors.New("session reaper unavailable")
	transientReaperErrors       = [...]error{
		errSessionReaperUnavailable,
		io.EOF,
		io.ErrUnexpectedEOF,
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.ECONNABORTED,
		syscall.ETIMEDOUT,
	}
)

type sessionReaper struct {
	endpoint string
	version  string
}

func ensureVerifiedReaperClient(ctx context.Context) error {
	if verifiedReaperConn != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, verifyReaperTimeout)
	defer cancel()

	conn, err := connectSessionReaper(ctx, findSessionReaper, registerReaperSessionFilters)
	if err != nil {
		return err
	}

	verifiedReaperConn = conn
	return nil
}

func connectSessionReaper(
	ctx context.Context,
	lookup func(context.Context) (sessionReaper, bool, error),
	register func(context.Context, sessionReaper) (net.Conn, error),
) (net.Conn, error) {
	// package binary 사이의 client 공백에 Ryuk 교체가 시작될 수 있으므로,
	// 교체 중 상태와 transport 오류에만 endpoint 재조회를 허용합니다.
	for {
		conn, err := tryConnectSessionReaper(ctx, lookup, register)
		if err == nil {
			return conn, nil
		}
		if !isTransientReaperError(err) {
			return nil, err
		}
		if waitErr := waitReaperRetry(ctx); waitErr != nil {
			return nil, errors.Join(err, waitErr)
		}
	}
}

func tryConnectSessionReaper(
	ctx context.Context,
	lookup func(context.Context) (sessionReaper, bool, error),
	register func(context.Context, sessionReaper) (net.Conn, error),
) (net.Conn, error) {
	reaper, found, err := lookup(ctx)
	if err != nil {
		return nil, fmt.Errorf("find session reaper: %w", err)
	}
	if !found {
		return nil, errSessionReaperUnavailable
	}

	conn, err := register(ctx, reaper)
	if err != nil {
		return nil, fmt.Errorf("register reaper session filters: %w", err)
	}
	return conn, nil
}

func waitReaperRetry(ctx context.Context) error {
	timer := time.NewTimer(reaperRetryInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func isTransientReaperError(err error) bool {
	for _, target := range transientReaperErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func findSessionReaper(ctx context.Context) (_ sessionReaper, _ bool, err error) {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return sessionReaper{}, false, fmt.Errorf("new docker provider: %w", err)
	}
	defer func() {
		if closeErr := provider.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close docker provider: %w", closeErr))
		}
	}()

	resp, err := provider.Client().ContainerList(ctx, client.ContainerListOptions{
		All: true,
		Filters: make(client.Filters).
			Add("label", "org.testcontainers.ryuk=true").
			Add("label", "org.testcontainers.sessionId="+testcontainers.SessionID()),
	})
	if err != nil {
		return sessionReaper{}, false, fmt.Errorf("list session reaper containers: %w", err)
	}

	return sessionReaperFromList(ctx, provider, resp)
}

func sessionReaperFromList(
	ctx context.Context,
	provider *testcontainers.DockerProvider,
	resp client.ContainerListResult,
) (sessionReaper, bool, error) {
	if len(resp.Items) == 0 {
		return sessionReaper{}, false, nil
	}

	item := resp.Items[0]
	if item.State != container.StateRunning {
		return sessionReaper{}, false, fmt.Errorf(
			"%w: container %.12s is %q, not running", errSessionReaperUnavailable, item.ID, item.State)
	}

	host, err := provider.DaemonHost(ctx)
	if err != nil {
		return sessionReaper{}, false, fmt.Errorf("daemon host: %w", err)
	}
	for _, port := range item.Ports {
		if port.PublicPort != 0 {
			return sessionReaper{
				endpoint: net.JoinHostPort(host, strconv.Itoa(int(port.PublicPort))),
				version:  item.Labels[reaperVersionLabel],
			}, true, nil
		}
	}
	return sessionReaper{}, false, fmt.Errorf("%w: no published port", errSessionReaperUnavailable)
}

func registerReaperSessionFilters(ctx context.Context, reaper sessionReaper) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", reaper.endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial reaper %s: %w", reaper.endpoint, err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, closeOnError(fmt.Errorf("set handshake deadline: %w", err), conn)
		}
	}

	if _, err := conn.Write([]byte(reaperSessionFilterLine(reaper.version))); err != nil {
		return nil, closeOnError(fmt.Errorf("write session filters: %w", err), conn)
	}

	ack := make([]byte, len(reaperAck))
	if _, err := io.ReadFull(conn, ack); err != nil {
		return nil, closeOnError(fmt.Errorf("read reaper ack: %w", err), conn)
	}
	if string(ack) != reaperAck {
		return nil, closeOnError(fmt.Errorf("unexpected reaper response: %q", ack), conn)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, closeOnError(fmt.Errorf("clear handshake deadline: %w", err), conn)
	}
	return conn, nil
}

func reaperSessionFilterLine(version string) string {
	line := "label=org.testcontainers=true" +
		"&label=org.testcontainers.lang=go" +
		"&label=org.testcontainers.reap=true" +
		"&label=org.testcontainers.sessionId=" + testcontainers.SessionID()
	if version != "" {
		line += "&label=" + reaperVersionLabel + "=" + version
	}
	return line + "\n"
}

func closeOnError(err error, conn net.Conn) error {
	if closeErr := conn.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("close reaper conn: %w", closeErr))
	}
	return err
}
