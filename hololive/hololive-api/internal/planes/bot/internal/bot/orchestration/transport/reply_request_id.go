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
	"fmt"
	"strconv"
	"strings"

	iris "github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
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

// replyReissueLadder는 스택 공통 bounded reissue 사다리다. 세대 상한과 :rN 규칙은 iris-client-go가
// 소유하고, hololive는 base가 길이 제약으로 :rN을 못 붙일 때의 hashed base 파생만 더한다.
var replyReissueLadder = irisdurable.ReissueLadder{
	MaxGenerations: iris.ReplyReissueMaxGenerations,
	Derive:         reissuedReplyClientRequestID,
}

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

// reissuedReplyClientRequestID는 replyReissueLadder의 Derive다. 범위 밖 세대, 이미 재발급된 base,
// 빈 base는 오류로 남겨 clientRequestId 없이 재전송되는 일이 없게 한다.
func reissuedReplyClientRequestID(clientRequestID string, generation int) (string, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)

	candidate, err := iris.ReissuedClientRequestID(clientRequestID, generation)
	if err == nil {
		return candidate, nil
	}

	if clientRequestID == "" ||
		errors.Is(err, iris.ErrReplyReissueGenerationOutOfRange) ||
		errors.Is(err, iris.ErrReplyReissueBaseAlreadyReissued) {
		return "", fmt.Errorf("reissue reply clientRequestId: %w", err)
	}

	fallbackBase := formatReplyClientRequestID(hashedReplyIDToken(clientRequestID), 0)

	candidate, err = iris.ReissuedClientRequestID(fallbackBase, generation)
	if err != nil {
		return "", fmt.Errorf("reissue hashed reply clientRequestId: %w", err)
	}

	return candidate, nil
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
