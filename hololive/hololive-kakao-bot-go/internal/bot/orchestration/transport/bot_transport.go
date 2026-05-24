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
	"strings"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
)

const serviceNameIris = "iris"

const (
	replyStatusPollInterval = 250 * time.Millisecond
	replySendMaxAttempts    = 2
)

var replyClientRequestSequence atomic.Uint64

type acceptedMessageSender interface {
	SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

type CommandTransport struct {
	irisClient IrisTransportClient
	formatter  *adapter.ResponseFormatter
}

func NewCommandTransport(irisClient IrisTransportClient, formatter *adapter.ResponseFormatter) *CommandTransport {
	return &CommandTransport{
		irisClient: irisClient,
		formatter:  formatter,
	}
}

func (t *CommandTransport) SendMessage(ctx context.Context, room, message string) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send message: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	var opts []iris.SendOption
	threadID := ""

	if id, ok := ThreadIDFromContext(sendCtx); ok {
		threadID = id
		opts = append(opts, iris.WithThreadID(threadID))
	}

	clientRequestIDBase := commandReplyClientRequestIDBase(room, message, commandReplyIdentity(sendCtx, threadID))
	if err := t.sendMessage(sendCtx, room, message, clientRequestIDBase, opts...); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send message", serviceNameIris, "send_message", err)
		return fmt.Errorf("send message to room %s: %w", room, serviceErr)
	}

	return nil
}

func (t *CommandTransport) sendMessage(ctx context.Context, room, message string, clientRequestIDBase string, opts ...iris.SendOption) error {
	acceptedSender, ok := t.irisClient.(acceptedMessageSender)
	if !ok {
		return t.irisClient.SendMessage(ctx, room, message, appendReplyClientRequestID(opts, clientRequestIDBase, 1)...)
	}

	var lastErr error
	for attempt := 1; attempt <= replySendMaxAttempts; attempt++ {
		attemptOpts := appendReplyClientRequestID(opts, clientRequestIDBase, attempt)
		done, err := sendAcceptedMessageAttempt(ctx, acceptedSender, room, message, attemptOpts...)
		if err != nil && !isReplyStatusFailed(err) {
			return err
		}
		if done {
			return nil
		}

		lastErr = err
	}

	return lastErr
}

func appendReplyClientRequestID(opts []iris.SendOption, base string, attempt int) []iris.SendOption {
	next := make([]iris.SendOption, 0, len(opts)+1)
	next = append(next, opts...)
	next = append(next, iris.WithClientRequestID(fmt.Sprintf("%s:a%d", base, attempt)))
	return next
}

func commandReplyClientRequestIDBase(room, message, threadID string) string {
	identity := strings.TrimSpace(threadID)
	if identity == "" {
		sequence := replyClientRequestSequence.Add(1)
		identity = fmt.Sprintf("local:%d:%d", time.Now().UnixNano(), sequence)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"hololive-bot-command-reply-v1",
		strings.TrimSpace(room),
		identity,
		message,
	}, "\x00")))
	return "hololive-bot:reply:" + hex.EncodeToString(sum[:16])
}

func commandReplyIdentity(ctx context.Context, threadID string) string {
	if id := strings.TrimSpace(threadID); id != "" {
		return id
	}
	if id, ok := ReplyIdentityFromContext(ctx); ok {
		return id
	}
	return ""
}

func sendAcceptedMessageAttempt(ctx context.Context, sender acceptedMessageSender, room string, message string, opts ...iris.SendOption) (bool, error) {
	accepted, err := sender.SendMessageAccepted(ctx, room, message, opts...)
	if err != nil {
		return false, err
	}
	if accepted == nil || strings.TrimSpace(accepted.RequestID) == "" {
		return true, nil
	}

	err = waitForReplyHandoff(ctx, sender, accepted.RequestID)
	if err == nil {
		return true, nil
	}
	if isReplyStatusFailed(err) {
		return false, err
	}
	return true, nil
}

type replyStatusFailedError struct {
	requestID string
	detail    string
}

func (e replyStatusFailedError) Error() string {
	if strings.TrimSpace(e.detail) == "" {
		return fmt.Sprintf("iris reply %s failed", e.requestID)
	}
	return fmt.Sprintf("iris reply %s failed: %s", e.requestID, e.detail)
}

func isReplyStatusFailed(err error) bool {
	var failed replyStatusFailedError
	return errors.As(err, &failed)
}

func waitForReplyHandoff(ctx context.Context, client acceptedMessageSender, requestID string) error {
	ticker := time.NewTicker(replyStatusPollInterval)
	defer ticker.Stop()

	for {
		done, err := checkReplyHandoffStatus(ctx, client, requestID)
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		if waitReplyStatusPoll(ctx, ticker.C) {
			return nil
		}
	}
}

func checkReplyHandoffStatus(ctx context.Context, client acceptedMessageSender, requestID string) (bool, error) {
	status, err := client.GetReplyStatus(ctx, requestID)
	if err != nil || status == nil {
		return err != nil, nil
	}
	return replyHandoffStatusResult(requestID, status)
}

func replyHandoffStatusResult(requestID string, status *iris.ReplyStatusSnapshot) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(status.State)) {
	case "handoff_completed", "delivered", "sent":
		return true, nil
	case "failed":
		return true, replyStatusFailedError{requestID: requestID, detail: replyStatusDetail(status)}
	default:
		return false, nil
	}
}

func replyStatusDetail(status *iris.ReplyStatusSnapshot) string {
	if status.Detail == nil {
		return ""
	}
	return *status.Detail
}

func waitReplyStatusPoll(ctx context.Context, tick <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return true
	case <-tick:
		return false
	}
}

func (t *CommandTransport) SendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send image: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	opts = appendMediaClientRequestOptions(sendCtx, opts, "image", room, imageData)
	if _, err := t.irisClient.SendImage(sendCtx, room, imageData, opts...); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send image", serviceNameIris, "send_image", err)
		return fmt.Errorf("send image to room %s: %w", room, serviceErr)
	}

	return nil
}

func (t *CommandTransport) SendMultipleImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send multiple images: iris client is not configured")
	}
	if len(images) == 0 {
		return errors.New("send multiple images: images must not be empty")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	opts = appendMultipleImageClientRequestOptions(sendCtx, opts, room, images)
	if _, err := t.irisClient.SendMultipleImages(sendCtx, room, images, opts...); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send multiple images", serviceNameIris, "send_multiple_images", err)
		return fmt.Errorf("send multiple images to room %s: %w", room, serviceErr)
	}

	return nil
}

func appendMediaClientRequestOptions(ctx context.Context, opts []iris.SendOption, kind, room string, payload []byte) []iris.SendOption {
	threadID, _ := ThreadIDFromContext(ctx)
	base := commandReplyClientRequestIDBase(
		room,
		string(mediaPayloadDigest(kind, payload)),
		commandReplyIdentity(ctx, threadID),
	)
	next := make([]iris.SendOption, 0, len(opts)+2)
	next = append(next, iris.WithClientRequestID(fmt.Sprintf("%s:a1", base)))
	if threadID != "" {
		next = append(next, iris.WithThreadID(threadID))
	}
	next = append(next, opts...)
	return next
}

func appendMultipleImageClientRequestOptions(ctx context.Context, opts []iris.SendOption, room string, images [][]byte) []iris.SendOption {
	digest := sha256.New()
	for _, image := range images {
		digest.Write([]byte{0})
		digest.Write(mediaPayloadDigest("image", image))
	}
	threadID, _ := ThreadIDFromContext(ctx)
	base := commandReplyClientRequestIDBase(
		room,
		hex.EncodeToString(digest.Sum(nil)),
		commandReplyIdentity(ctx, threadID),
	)
	next := make([]iris.SendOption, 0, len(opts)+2)
	next = append(next, iris.WithClientRequestID(fmt.Sprintf("%s:a1", base)))
	if threadID != "" {
		next = append(next, iris.WithThreadID(threadID))
	}
	next = append(next, opts...)
	return next
}

func mediaPayloadDigest(kind string, payload []byte) []byte {
	sum := sha256.Sum256(append([]byte(kind+"\x00"), payload...))
	return sum[:]
}

func (t *CommandTransport) SendError(ctx context.Context, room, errorMsg string) error {
	message := errorMsg

	if t != nil && t.formatter != nil {
		message = t.formatter.FormatError(errorMsg)
	}

	if err := t.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send error message: %w", err)
	}

	return nil
}
