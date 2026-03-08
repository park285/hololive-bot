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

import "context"

type Client interface {
	SendMessage(ctx context.Context, room, message string, opts ...SendOption) error
	SendImage(ctx context.Context, room, imageBase64 string) error
	Ping(ctx context.Context) bool
	GetConfig(ctx context.Context) (*Config, error)
	Decrypt(ctx context.Context, data string) (string, error)
}

type SendOption func(*sendOptions)

type sendOptions struct {
	ThreadID *string
}

func WithThreadID(id string) SendOption {
	return func(o *sendOptions) { o.ThreadID = &id }
}

func applySendOptions(opts []SendOption) sendOptions {
	var o sendOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
