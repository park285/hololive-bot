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

package cache

import (
	"log/slog"
	"sync"

	"github.com/valkey-io/valkey-go"
)

// 기본 Key-Value 외에도 Set, Hash 등 다양한 자료구조 연산을 지원한다.
type Service struct {
	client    valkey.Client
	logger    *slog.Logger
	closeOnce sync.Once
}

var _ Client = (*Service)(nil)

type Config struct {
	Host       string
	Port       int
	Password   string
	DB         int
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
	// DisableCache: valkey-go client-side caching 비활성화 (miniredis/RESP2 호환)
	DisableCache      bool
	ForceSingleClient bool
}
