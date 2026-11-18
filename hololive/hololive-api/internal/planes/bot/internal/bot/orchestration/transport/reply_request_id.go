package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	iris "github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

const (
	replyIDService       = "hololive"
	replyIDSchemaVersion = "v1"
	replyPhaseReply      = "reply"
)

var replyReissueLadder = irisdurable.ReissueLadder{
	MaxGenerations: iris.ReplyReissueMaxGenerations,
	Derive:         reissuedReplyClientRequestID,
}

func replyClientRequestID(messageID string, ordinal uint64) string {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return ""
	}

	candidate := formatReplyClientRequestID(id, ordinal)
	if iris.ValidateClientRequestID(candidate) == nil {
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
