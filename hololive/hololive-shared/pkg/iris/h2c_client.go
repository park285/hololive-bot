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

package iris

import (
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/client"
)

// H2CClient: iris-client-go/client.H2CClient의 타입 별칭입니다.
type H2CClient = client.H2CClient

// H2CClientOptions: 기존 구조체 기반 옵션 — 하위 호환성 유지를 위해 보존합니다.
type H2CClientOptions struct {
	Timeout               time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	ReadIdleTimeout       time.Duration
	PingTimeout           time.Duration
	WriteByteTimeout      time.Duration
}

// NewH2CClient: 구조체 옵션을 함수형 옵션으로 변환하여 iris-client-go H2CClient를 생성합니다.
func NewH2CClient(baseURL, botToken string, logger *slog.Logger, options ...H2CClientOptions) *client.H2CClient {
	var opts []client.ClientOption
	if logger != nil {
		opts = append(opts, client.WithLogger(logger))
	}
	if len(options) > 0 {
		o := options[0]
		if o.Timeout > 0 {
			opts = append(opts, client.WithTimeout(o.Timeout))
		}
		if o.DialTimeout > 0 {
			opts = append(opts, client.WithDialTimeout(o.DialTimeout))
		}
		if o.TLSHandshakeTimeout > 0 {
			opts = append(opts, client.WithTLSHandshakeTimeout(o.TLSHandshakeTimeout))
		}
		if o.ResponseHeaderTimeout > 0 {
			opts = append(opts, client.WithResponseHeaderTimeout(o.ResponseHeaderTimeout))
		}
		if o.IdleConnTimeout > 0 {
			opts = append(opts, client.WithIdleConnTimeout(o.IdleConnTimeout))
		}
		if o.MaxIdleConns > 0 {
			opts = append(opts, client.WithMaxIdleConns(o.MaxIdleConns))
		}
		if o.MaxIdleConnsPerHost > 0 {
			opts = append(opts, client.WithMaxIdleConnsPerHost(o.MaxIdleConnsPerHost))
		}
		if o.ReadIdleTimeout > 0 {
			opts = append(opts, client.WithReadIdleTimeout(o.ReadIdleTimeout))
		}
		if o.PingTimeout > 0 {
			opts = append(opts, client.WithPingTimeout(o.PingTimeout))
		}
		if o.WriteByteTimeout > 0 {
			opts = append(opts, client.WithWriteByteTimeout(o.WriteByteTimeout))
		}
	}
	return client.NewH2CClient(baseURL, botToken, opts...)
}
