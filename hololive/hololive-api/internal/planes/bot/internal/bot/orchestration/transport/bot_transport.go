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

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const serviceNameIris = "iris"

const (
	replyStatusPollInterval = 250 * time.Millisecond
	replySendMaxAttempts    = 2
)

var replyClientRequestSequence atomic.Uint64

type replyStatusGetter interface {
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

type acceptedMessageSender interface {
	SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	replyStatusGetter
}

type replySendFunc func(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)

type replyLane struct {
	send   replySendFunc
	getter replyStatusGetter
}

type CommandTransport struct {
	irisClient      iris.BotClient
	formatter       *messageformatter.ResponseFormatter
	markdownReplies bool
}

type Option func(*CommandTransport)

func WithMarkdownReplies(enabled bool) Option {
	return func(t *CommandTransport) {
		t.markdownReplies = enabled
	}
}

func NewCommandTransport(irisClient iris.BotClient, formatter *messageformatter.ResponseFormatter, opts ...Option) *CommandTransport {
	t := &CommandTransport{
		irisClient: irisClient,
		formatter:  formatter,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}

	return t
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

	clientRequestIDBase := commandReplyClientRequestIDBase(room, message, commandReplyIdentity(sendCtx))
	if err := t.sendMessage(sendCtx, room, message, clientRequestIDBase, opts...); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send message", serviceNameIris, "send_message", err)
		return fmt.Errorf("send message to room %s: %w", room, serviceErr)
	}

	return nil
}

func (t *CommandTransport) sendMessage(ctx context.Context, room, message, clientRequestIDBase string, opts ...iris.SendOption) error {
	if t.markdownReplies {
		lane := replyLane{send: t.irisClient.SendMarkdown, getter: t.irisClient}
		return sendReplyWithAttempts(ctx, lane, room, message, clientRequestIDBase, opts)
	}

	acceptedSender, ok := t.irisClient.(acceptedMessageSender)
	if !ok {
		return t.irisClient.SendMessage(ctx, room, message, appendReplyClientRequestID(opts, clientRequestIDBase, 1)...)
	}

	lane := replyLane{send: acceptedSender.SendMessageAccepted, getter: acceptedSender}
	return sendReplyWithAttempts(ctx, lane, room, message, clientRequestIDBase, opts)
}

func sendReplyWithAttempts(ctx context.Context, lane replyLane, room, message, clientRequestIDBase string, opts []iris.SendOption) error {
	var lastErr error
	for attempt := 1; attempt <= replySendMaxAttempts; attempt++ {
		attemptOpts := appendReplyClientRequestID(opts, clientRequestIDBase, attempt)
		done, err := sendReplyAttempt(ctx, lane, room, message, attemptOpts...)
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

func commandReplyClientRequestIDBase(room, message, replyIdentity string) string {
	identity := strings.TrimSpace(replyIdentity)
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

func commandReplyIdentity(ctx context.Context) string {
	if id, ok := ReplyIdentityFromContext(ctx); ok {
		return id
	}
	return ""
}

func sendReplyAttempt(ctx context.Context, lane replyLane, room, message string, opts ...iris.SendOption) (bool, error) {
	accepted, err := lane.send(ctx, room, message, opts...)
	if err != nil {
		return false, err
	}

	err = waitForAcceptedReplyHandoff(ctx, lane.getter, accepted)
	if err == nil {
		return true, nil
	}
	if isReplyStatusFailed(err) {
		return false, err
	}
	return true, nil
}

func waitForAcceptedReplyHandoff(ctx context.Context, getter replyStatusGetter, accepted *iris.ReplyAcceptedResponse) error {
	if accepted == nil || strings.TrimSpace(accepted.RequestID) == "" {
		return nil
	}
	return waitForReplyHandoff(ctx, getter, accepted.RequestID)
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

func waitForReplyHandoff(ctx context.Context, client replyStatusGetter, requestID string) error {
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

func checkReplyHandoffStatus(ctx context.Context, client replyStatusGetter, requestID string) (bool, error) {
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
	accepted, err := t.irisClient.SendImage(sendCtx, room, imageData, opts...)
	if err == nil {
		err = waitForAcceptedReplyHandoff(sendCtx, t.irisClient, accepted)
	}
	if err != nil {
		serviceErr := appErrors.NewServiceError("failed to send image", serviceNameIris, "send_image", err)
		return fmt.Errorf("send image to room %s: %w", room, serviceErr)
	}

	return nil
}

func (t *CommandTransport) SendImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) error {
	if len(images) == 1 {
		return t.SendImage(ctx, room, images[0], opts...)
	}
	if t == nil || t.irisClient == nil {
		return errors.New("send images: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	opts = appendMediaClientRequestOptions(sendCtx, opts, "image_multiple", room, imageBatchPayload(images))
	accepted, err := t.irisClient.SendMultipleImages(sendCtx, room, images, opts...)
	if err == nil {
		err = waitForAcceptedReplyHandoff(sendCtx, t.irisClient, accepted)
	}
	if err != nil {
		serviceErr := appErrors.NewServiceError("failed to send images", serviceNameIris, "send_images", err)
		return fmt.Errorf("send images to room %s: %w", room, serviceErr)
	}

	return nil
}

func appendMediaClientRequestOptions(ctx context.Context, opts []iris.SendOption, kind, room string, payload []byte) []iris.SendOption {
	threadID, _ := ThreadIDFromContext(ctx)
	base := commandReplyClientRequestIDBase(
		room,
		string(mediaPayloadDigest(kind, payload)),
		commandReplyIdentity(ctx),
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

func imageBatchPayload(images [][]byte) []byte {
	payload := make([]byte, 0, len(images)*sha256.Size)
	for _, imageData := range images {
		sum := sha256.Sum256(imageData)
		payload = append(payload, sum[:]...)
	}
	return payload
}

func (t *CommandTransport) SendError(ctx context.Context, room, key string) error {
	message := messagestrings.FallbackSentinel

	if t != nil && t.formatter != nil {
		message = t.formatter.ResolveError(ctx, key)
	}

	if err := t.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send error message: %w", err)
	}

	return nil
}
