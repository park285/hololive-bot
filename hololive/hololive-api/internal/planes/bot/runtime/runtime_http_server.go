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

package botruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

func (r *BotRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil {
		return
	}

	r.requestsMu.Lock()
	defer r.requestsMu.Unlock()

	if r.requestsClosing {
		return
	}

	r.prepareHTTPHandlers()

	startHTTP3Server(r.H3Server, r.Logger, errCh)
	startShortLinkServer(r.ShortLinkServer, r.Logger, errCh)
	startMetricsServer(r.MetricsServer, r.Logger, errCh)
	startPprofServer(r.PprofServer, r.Logger, errCh)
}

func (r *BotRuntime) prepareHTTPHandlers() {
	r.httpHandlersOnce.Do(func() {
		if r.H3Server != nil {
			r.H3Server.Handler = r.trackHTTPHandler(r.H3Server.Handler)
		}

		for _, server := range []*http.Server{r.ShortLinkServer, r.MetricsServer, r.PprofServer} {
			if server != nil {
				server.Handler = r.trackHTTPHandler(server.Handler)
			}
		}
	})
}

func (r *BotRuntime) trackHTTPHandler(next http.Handler) http.Handler {
	if next == nil {
		next = http.DefaultServeMux
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requestsMu.Lock()

		if r.requestsClosing {
			r.requestsMu.Unlock()
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)

			return
		}

		r.requestsWG.Add(1)
		r.requestsMu.Unlock()

		defer r.requestsWG.Done()

		next.ServeHTTP(w, req)
	})
}

func (r *BotRuntime) stopHTTPRequestAdmission() {
	r.requestsMu.Lock()
	defer r.requestsMu.Unlock()

	r.requestsClosing = true

	if r.requestsDone == nil {
		r.requestsDone = make(chan struct{})

		go func() {
			r.requestsWG.Wait()
			close(r.requestsDone)
		}()
	}
}

func (r *BotRuntime) joinHTTPRequests(ctx context.Context) error {
	r.requestsMu.Lock()

	done := r.requestsDone
	r.requestsMu.Unlock()

	if err := waitForDurableStop(ctx, done); err != nil {
		return fmt.Errorf("join bot HTTP requests: %w", err)
	}

	return nil
}

func (r *BotRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil {
		return nil
	}

	return errors.Join(
		shutdownHTTP3Server(ctx, r.H3Server),
		shutdownShortLinkServer(ctx, r.ShortLinkServer),
		shutdownMetricsServer(ctx, r.MetricsServer),
		shutdownPprofServer(ctx, r.PprofServer),
	)
}
