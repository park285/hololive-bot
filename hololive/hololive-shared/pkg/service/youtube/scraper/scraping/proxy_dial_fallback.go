package scraping

import (
	"context"
	"errors"
	"fmt"
	"net"

	"golang.org/x/net/proxy"

	"github.com/kapu/hololive-shared/pkg/panicguard"
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
		drainCanceledSOCKS5Dial(done)

		return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
	case result := <-done:
		out, err := finishSOCKS5DialResult(ctx, result)
		if err != nil {
			return nil, fmt.Errorf("finish SOCKS5 dial result: %w", err)
		}

		return out, nil
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

func drainCanceledSOCKS5Dial(done <-chan dialResult) {
	select {
	case result := <-done:
		closeConnQuietly(result.conn)
	default:
	}
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
		if err := conn.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}

		return nil
	}

	return nil
}

func closeConnQuietly(conn net.Conn) {
	if err := closeConn(conn); err != nil {
		return
	}
}
