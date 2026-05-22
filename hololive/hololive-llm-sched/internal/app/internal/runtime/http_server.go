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

	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/httpserver"
)

type listenErrorPrefixServer struct {
	httpserver.Server
	errorText string
	logger    *slog.Logger
	errCh     chan<- error
}

func (s listenErrorPrefixServer) ListenAndServe() error {
	err := s.Server.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return err
	}

	if s.errCh == nil && s.logger != nil {
		s.logger.Error(s.errorText, slog.Any("error", err))
	}

	return fmt.Errorf("%s: %w", s.errorText, err)
}

func StartHTTPServer(server *http.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	httpserver.Start(listenErrorPrefixServer{
		Server:    server,
		errorText: "HTTP server error",
		logger:    logger,
		errCh:     errCh,
	}, nil, errCh)
}

func ShutdownHTTPServer(server *http.Server, ctx context.Context) error {
	if server == nil {
		return nil
	}

	return httpserver.Shutdown(ctx, server, "HTTP server shutdown failed")
}
