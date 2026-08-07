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

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	iris "github.com/park285/iris-client-go/iris"
)

const (
	replyIDService       = "hololive"
	replyIDSchemaVersion = "v1"
	replyPhaseReply      = "reply"
)

// iris.WithClientRequestID 계약: 8..160 ASCII, [A-Za-z0-9._:-]만 허용.
const (
	replyClientRequestIDMinLen = 8
	replyClientRequestIDMaxLen = 160
)

func nextReplyClientRequestID(ctx context.Context) string {
	identity, ordinal, ok := nextReplyEmission(ctx)
	if !ok {
		return ""
	}

	return replyClientRequestID(identity, ordinal)
}

func replyClientRequestID(messageID string, ordinal uint64) string {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return ""
	}

	candidate := formatReplyClientRequestID(id, ordinal)
	if isValidReplyClientRequestID(candidate) {
		return candidate
	}

	return formatReplyClientRequestID(hashedReplyIDToken(id), ordinal)
}

func formatReplyClientRequestID(token string, ordinal uint64) string {
	return strings.Join([]string{
		replyIDService,
		replyIDSchemaVersion,
		token,
		replyPhaseReply,
		strconv.FormatUint(ordinal, 10),
	}, ":")
}

func reissuedReplyClientRequestID(clientRequestID string, generation int) string {
	clientRequestID = strings.TrimSpace(clientRequestID)
	if generation <= 0 {
		return clientRequestID
	}
	if clientRequestID == "" {
		return ""
	}

	candidate, err := iris.ReissuedClientRequestID(clientRequestID, generation)
	if err == nil {
		return candidate
	}
	if errors.Is(err, iris.ErrReplyReissueGenerationOutOfRange) ||
		errors.Is(err, iris.ErrReplyReissueBaseAlreadyReissued) {
		return ""
	}

	fallbackBase := formatReplyClientRequestID(hashedReplyIDToken(clientRequestID), 0)
	candidate, err = iris.ReissuedClientRequestID(fallbackBase, generation)
	if err != nil {
		return ""
	}
	return candidate
}

func hashedReplyIDToken(messageID string) string {
	sum := sha256.Sum256([]byte(messageID))
	return "h" + hex.EncodeToString(sum[:16])
}

func isValidReplyClientRequestID(id string) bool {
	if len(id) < replyClientRequestIDMinLen || len(id) > replyClientRequestIDMaxLen {
		return false
	}

	for _, r := range id {
		if !isReplyClientRequestIDRune(r) {
			return false
		}
	}

	return true
}

func isReplyClientRequestIDRune(r rune) bool {
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:-"
	return strings.ContainsRune(allowed, r)
}
