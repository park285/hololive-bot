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

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/quic-go/quic-go/http3"
)

func StartHTTPServer(server *http.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if errCh != nil {
				errCh <- fmt.Errorf("HTTP server error: %w", err)
				return
			}

			if logger != nil {
				logger.Error("HTTP server error", slog.Any("error", err))
			}
		}
	}()
}

func StartHTTP3Server(server *http3.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if errCh != nil {
				errCh <- fmt.Errorf("HTTP/3 server error: %w", err)
				return
			}

			if logger != nil {
				logger.Error("HTTP/3 server error", slog.Any("error", err))
			}
		}
	}()
}

func ShutdownHTTPServer(server *http.Server, ctx context.Context) error {
	if server == nil {
		return nil
	}

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}

	return nil
}

func ShutdownHTTP3Server(server *http3.Server, ctx context.Context) error {
	if server == nil {
		return nil
	}

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP/3 server shutdown failed: %w", err)
	}

	return nil
}
