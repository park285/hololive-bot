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
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/iris-client-go/iris"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const serviceNameIris = "iris"

const (
	replyStatusPollInterval   = 250 * time.Millisecond
	replyAdmissionMaxAttempts = 2
)

type replyStatusGetter interface {
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

type acceptedMessageSender interface {
	SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	replyStatusGetter
}

type replySendFunc func(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
type replyAdmissionFunc func(ctx context.Context, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)

type replyLane struct {
	send   replySendFunc
	getter replyStatusGetter
}

type CommandTransport struct {
	irisClient      iris.BotClient
	formatter       *messageformatter.ResponseFormatter
	markdownReplies bool
	replyOutbox     ReplyOutboxWriter
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

	opts := appendThreadIDOption(sendCtx, nil)

	clientRequestID := nextReplyClientRequestID(sendCtx)
	if t.replyOutboxWriter() != nil {
		kind := StoredReplyKindText
		if t.markdownReplies {
			kind = StoredReplyKindMarkdown
		}
		threadID, _ := ThreadIDFromContext(sendCtx)
		if err := t.recordReply(sendCtx, room, clientRequestID, &StoredReply{Kind: kind, Message: message, ThreadID: threadID}); err != nil {
			return fmt.Errorf("record reply: %w", err)
		}
		return nil
	}
	if err := t.sendMessage(sendCtx, room, message, clientRequestID, opts...); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send message", serviceNameIris, "send_message", err)
		return fmt.Errorf("send message: %w", serviceErr)
	}

	return nil
}

func (t *CommandTransport) sendMessage(ctx context.Context, room, message, clientRequestID string, opts ...iris.SendOption) error {
	if t.markdownReplies {
		lane := replyLane{send: t.irisClient.SendMarkdown, getter: t.irisClient}
		return sendReply(ctx, lane, room, message, clientRequestID, opts)
	}

	acceptedSender, ok := t.irisClient.(acceptedMessageSender)
	if !ok {
		return fmt.Errorf("send reply: iris client %T cannot report reply admission or status", t.irisClient)
	}

	lane := replyLane{send: acceptedSender.SendMessageAccepted, getter: acceptedSender}
	return sendReply(ctx, lane, room, message, clientRequestID, opts)
}

func sendReply(ctx context.Context, lane replyLane, room, message, clientRequestID string, opts []iris.SendOption) error {
	accepted, err := postReplyWithReissue(ctx, clientRequestID, func(generationID string) (*iris.ReplyAcceptedResponse, error) {
		return postReply(ctx, lane.send, room, message, generationID, appendReplyClientRequestID(opts, generationID))
	})
	if err != nil {
		return err
	}

	return waitForAcceptedReplyHandoff(ctx, lane.getter, accepted)
}

func postLiveMediaWithReissue(
	ctx context.Context,
	clientRequestID string,
	opts []iris.SendOption,
	post replyAdmissionFunc,
) (*iris.ReplyAcceptedResponse, error) {
	return postReplyWithReissue(ctx, clientRequestID, func(generationID string) (*iris.ReplyAcceptedResponse, error) {
		return post(ctx, appendReplyClientRequestID(opts, generationID)...)
	})
}

func postReplyWithReissue(
	ctx context.Context,
	clientRequestID string,
	post func(string) (*iris.ReplyAcceptedResponse, error),
) (*iris.ReplyAcceptedResponse, error) {
	var lastErr error
	for generation := 0; generation <= iris.ReplyReissueMaxGenerations; generation++ {
		accepted, err := post(reissuedReplyClientRequestID(clientRequestID, generation))
		if !isReplyReissueConflict(err) {
			return accepted, err
		}
		lastErr = err
		if clientRequestID == "" || ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func postReply(
	ctx context.Context,
	send replySendFunc,
	room, message, clientRequestID string,
	opts []iris.SendOption,
) (*iris.ReplyAcceptedResponse, error) {
	var lastErr error
	admissionMayHaveLanded := false

	for attempt := 1; attempt <= replyAdmissionMaxAttempts; attempt++ {
		accepted, err := send(ctx, room, message, opts...)
		if err == nil {
			return accepted, nil
		}

		lastErr = err
		abort, stop := replyAdmissionFailureAction(ctx, err, admissionMayHaveLanded, clientRequestID)
		if abort {
			return nil, err
		}
		if stop {
			break
		}
		admissionMayHaveLanded = true
	}

	return nil, replyOutcomeUnknownError{reason: "reply admission response was not received", cause: lastErr}
}

func replyAdmissionFailureAction(ctx context.Context, err error, admissionMayHaveLanded bool, clientRequestID string) (abort, stop bool) {
	if isReplyReissueConflict(err) {
		return true, false
	}
	s, definitive := stopReplyAdmissionRetry(ctx, err, admissionMayHaveLanded, clientRequestID)
	return definitive, s
}

func stopReplyAdmissionRetry(ctx context.Context, err error, admissionMayHaveLanded bool, clientRequestID string) (stop, definitive bool) {
	if !admissionResponseLost(err) {
		return true, !admissionMayHaveLanded
	}
	return clientRequestID == "" || ctx.Err() != nil, false
}

func admissionResponseLost(err error) bool {
	return errors.Is(err, iris.ErrTransport)
}

func classifyAdmissionError(err error) error {
	if !admissionResponseLost(err) {
		return err
	}

	return replyOutcomeUnknownError{reason: "reply admission response was not received", cause: err}
}

func isReplyReissueConflict(err error) bool {
	return iris.IsPreHandoffClientRequestIDConflict(err)
}

func appendReplyClientRequestID(opts []iris.SendOption, clientRequestID string) []iris.SendOption {
	next := make([]iris.SendOption, 0, len(opts)+1)
	next = append(next, opts...)
	if clientRequestID != "" {
		next = append(next, iris.WithClientRequestID(clientRequestID))
	}

	return next
}

func (t *CommandTransport) SendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send image: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if t.replyOutboxWriter() != nil {
		clientRequestID := nextReplyClientRequestID(sendCtx)
		threadID, _ := ThreadIDFromContext(sendCtx)
		contentType, _ := ImageContentTypeFromContext(sendCtx)
		if err := t.recordReply(sendCtx, room, clientRequestID, &StoredReply{Kind: StoredReplyKindImage, Image: imageData, ThreadID: threadID, ImageContentType: contentType}); err != nil {
			return fmt.Errorf("record image reply: %w", err)
		}
		return nil
	}

	if contentType, ok := ImageContentTypeFromContext(sendCtx); ok {
		opts = append(opts, iris.WithImageContentType(contentType))
	}
	clientRequestID, opts := liveMediaRequestOptions(sendCtx, opts)
	accepted, err := postLiveMediaWithReissue(sendCtx, clientRequestID, opts, func(ctx context.Context, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
		return t.irisClient.SendImage(ctx, room, imageData, opts...)
	})
	if err != nil {
		err = classifyAdmissionError(err)
	} else {
		err = waitForAcceptedReplyHandoff(sendCtx, t.irisClient, accepted)
	}
	if err != nil {
		serviceErr := appErrors.NewServiceError("failed to send image", serviceNameIris, "send_image", err)
		return fmt.Errorf("send image: %w", serviceErr)
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

	if t.replyOutboxWriter() != nil {
		clientRequestID := nextReplyClientRequestID(sendCtx)
		threadID, _ := ThreadIDFromContext(sendCtx)
		if err := t.recordReply(sendCtx, room, clientRequestID, &StoredReply{Kind: StoredReplyKindImages, Images: images, ThreadID: threadID}); err != nil {
			return fmt.Errorf("record image replies: %w", err)
		}
		return nil
	}

	clientRequestID, opts := liveMediaRequestOptions(sendCtx, opts)
	accepted, err := postLiveMediaWithReissue(sendCtx, clientRequestID, opts, func(ctx context.Context, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
		return t.irisClient.SendMultipleImages(ctx, room, images, opts...)
	})
	if err != nil {
		err = classifyAdmissionError(err)
	} else {
		err = waitForAcceptedReplyHandoff(sendCtx, t.irisClient, accepted)
	}
	if err != nil {
		serviceErr := appErrors.NewServiceError("failed to send images", serviceNameIris, "send_images", err)
		return fmt.Errorf("send images: %w", serviceErr)
	}

	return nil
}

func liveMediaRequestOptions(ctx context.Context, opts []iris.SendOption) (clientRequestID string, next []iris.SendOption) {
	clientRequestID = nextReplyClientRequestID(ctx)
	next = make([]iris.SendOption, 0, len(opts)+1)
	if threadID, ok := ThreadIDFromContext(ctx); ok {
		next = append(next, iris.WithThreadID(threadID))
	}

	return clientRequestID, append(next, opts...)
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
