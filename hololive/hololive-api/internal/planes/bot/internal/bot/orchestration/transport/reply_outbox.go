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

package transport

import "context"

const ReplyPhase = replyPhaseReply

type ReplyOutboxEntry struct {
	MessageID       string
	Phase           string
	Ordinal         uint64
	Room            string
	Payload         string
	ClientRequestID string
}

type ReplyOutboxWriter interface {
	RecordReply(ctx context.Context, entry *ReplyOutboxEntry) error
}

func WithReplyOutboxWriter(writer ReplyOutboxWriter) Option {
	return func(t *CommandTransport) {
		t.replyOutbox = writer
	}
}

func (t *CommandTransport) replyOutboxWriter() ReplyOutboxWriter {
	if t == nil {
		return nil
	}

	return t.replyOutbox
}

func ReplyClientRequestID(identity string, ordinal uint64) string {
	return replyClientRequestID(identity, ordinal)
}
