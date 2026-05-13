package scraper

import (
	"context"
	"fmt"
	"net"

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
	go func() {
		runSOCKS5Dial(ctx, dialer, network, addr, done)
	}()
}

func runSOCKS5Dial(ctx context.Context, dialer proxy.Dialer, network, addr string, done chan<- dialResult) {
	conn, err := dialer.Dial(network, addr)
	if ctx.Err() != nil {
		closeConn(conn)
		return
	}
	sendSOCKS5DialResult(done, dialResult{conn: conn, err: err})
}

func sendSOCKS5DialResult(done chan<- dialResult, result dialResult) {
	select {
	case done <- result:
	default:
		closeConn(result.conn)
	}
}

func finishCanceledSOCKS5Dial(ctx context.Context, done <-chan dialResult) (net.Conn, error) {
	select {
	case result := <-done:
		closeConn(result.conn)
	default:
	}
	return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
}

func finishSOCKS5DialResult(ctx context.Context, result dialResult) (net.Conn, error) {
	if result.err != nil {
		return nil, fmt.Errorf("proxy dial failed: %w", result.err)
	}
	if ctx.Err() != nil {
		closeConn(result.conn)
		return nil, fmt.Errorf("proxy dial canceled: %w", ctx.Err())
	}
	return result.conn, nil
}

func closeConn(conn net.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
}
