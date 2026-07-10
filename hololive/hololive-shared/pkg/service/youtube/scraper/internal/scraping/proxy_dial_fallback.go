package scraping

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	"golang.org/x/net/proxy"
)

type dialResult struct {
	conn net.Conn
	err  error
}

func dialSOCKS5WithContextFallback(ctx context.Context, dialer proxy.Dialer, network, addr string) (net.Conn, error) {
	done := make(chan dialResult, 1)
	startSOCKS5Dial(ctx, dialer, network, addr, done)

	select {
	case <-ctx.Done():
		return finishCanceledSOCKS5Dial(ctx, done)
	case result := <-done:
		return finishSOCKS5DialResult(ctx, result)
	}
}

func startSOCKS5Dial(ctx context.Context, dialer proxy.Dialer, network, addr string, done chan<- dialResult) {
	panicguard.Go(nil, "socks5-dial", func() {
		err := panicguard.RunE(nil, "socks5-dial", func() error {
			runSOCKS5Dial(ctx, dialer, network, addr, done)
			return nil
		})
		if err != nil {
			sendSOCKS5DialResult(done, dialResult{err: err})
		}
	})
}

func runSOCKS5Dial(ctx context.Context, dialer proxy.Dialer, network, addr string, done chan<- dialResult) {
	conn, err := dialer.Dial(network, addr)
	if ctx.Err() != nil {
		closeConnQuietly(conn)
		return
	}
	sendSOCKS5DialResult(done, dialResult{conn: conn, err: err})
}

func sendSOCKS5DialResult(done chan<- dialResult, result dialResult) {
	select {
	case done <- result:
	default:
		closeConnQuietly(result.conn)
	}
}

func finishCanceledSOCKS5Dial(ctx context.Context, done <-chan dialResult) (net.Conn, error) {
	select {
	case result := <-done:
		closeConnQuietly(result.conn)
	default:
	}
	return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
}

func finishSOCKS5DialResult(ctx context.Context, result dialResult) (net.Conn, error) {
	if result.err != nil {
		return nil, fmt.Errorf("proxy dial failed: %w", result.err)
	}
	if ctx.Err() != nil {
		if closeErr := closeConn(result.conn); closeErr != nil {
			return nil, errors.Join(fmt.Errorf("proxy dial canceled: %w", ctx.Err()), fmt.Errorf("close proxy connection: %w", closeErr))
		}
		return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
	}
	return result.conn, nil
}

func closeConn(conn net.Conn) error {
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func closeConnQuietly(conn net.Conn) {
	if err := closeConn(conn); err != nil {
		return
	}
}
