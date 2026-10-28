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
	"strings"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// CommandTransport는 명령 응답을 durable reply outbox에 기록한다. Iris 발송은 outbox dispatcher가
// DispatchStoredReply로만 하므로, 조립 실수로 원장 없이 즉시 발송하는 경로가 없다.
type CommandTransport struct {
	formatter       *messageformatter.ResponseFormatter
	markdownReplies bool
	rooms           RoomChat
	replyOutbox     ReplyOutboxWriter
}

type Option func(*CommandTransport)

func WithMarkdownReplies(enabled bool) Option {
	return func(t *CommandTransport) {
		t.markdownReplies = enabled
	}
}

func WithRoomChatLookup(rooms RoomChat) Option {
	return func(t *CommandTransport) {
		t.rooms = rooms
	}
}

func NewCommandTransport(replyOutbox ReplyOutboxWriter, formatter *messageformatter.ResponseFormatter, opts ...Option) *CommandTransport {
	t := &CommandTransport{
		formatter:   formatter,
		replyOutbox: replyOutbox,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}

	return t
}

func (t *CommandTransport) SendMessage(ctx context.Context, room, message string) error {
	if t == nil {
		return errors.New("send message: transport is not configured")
	}

	useMarkdown := markdownForRoom(ctx, room, t.markdownReplies, t.rooms)
	if !useMarkdown {
		message = kakaoformat.Render(message)
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	return t.recordTextReply(sendCtx, room, message, issueReplyEmission(sendCtx), useMarkdown)
}

func (t *CommandTransport) recordTextReply(
	ctx context.Context,
	room, message string,
	emission replyEmission,
	useMarkdown bool,
) error {
	kind := StoredReplyKindText

	if useMarkdown {
		kind = StoredReplyKindMarkdown
	}

	threadID, _ := ThreadIDFromContext(ctx)
	if err := t.recordReply(ctx, room, emission, &StoredReply{Kind: kind, Message: message, ThreadID: threadID}); err != nil {
		return fmt.Errorf("record reply: %w", err)
	}

	return nil
}

// postReplyWithReissue는 pre-handoff conflict에서만 replyReissueLadder로 세대를 올린다. Conflict는
// 원문 오류 그대로 돌려줘 호출자가 Iris code로 정산하고, clientRequestId가 없는 발송은 재발급할
// 정체성이 없으므로 한 번만 보낸다.
func postReplyWithReissue(
	ctx context.Context,
	clientRequestID string,
	post func(string) (*iris.ReplyAcceptedResponse, error),
) (*iris.ReplyAcceptedResponse, error) {
	clientRequestID = strings.TrimSpace(clientRequestID)

	if clientRequestID == "" {
		accepted, err := post("")
		if err != nil {
			return nil, replyPostError(err)
		}

		return accepted, nil
	}

	var accepted *iris.ReplyAcceptedResponse

	_, err := replyReissueLadder.Run(ctx, clientRequestID, 0, func(_ context.Context, generationID string, _ int) error {
		out, err := post(generationID)
		if err != nil {
			return err
		}

		accepted = out

		return nil
	}, isReplyReissueConflict)
	if err != nil {
		return nil, replyPostError(err)
	}

	return accepted, nil
}

func replyPostError(err error) error {
	if isReplyReissueConflict(err) {
		return err
	}

	return fmt.Errorf("post reply: %w", err)
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

func (t *CommandTransport) SendImage(ctx context.Context, room string, imageData []byte) error {
	if t == nil {
		return errors.New("send image: transport is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	emission := issueReplyEmission(sendCtx)
	threadID, _ := ThreadIDFromContext(sendCtx)
	contentType, _ := ImageContentTypeFromContext(sendCtx)

	if err := t.recordReply(sendCtx, room, emission, &StoredReply{Kind: StoredReplyKindImage, Image: imageData, ThreadID: threadID, ImageContentType: contentType}); err != nil {
		return fmt.Errorf("record image reply: %w", err)
	}

	return nil
}

func (t *CommandTransport) SendImages(ctx context.Context, room string, images [][]byte) error {
	if len(images) == 1 {
		if err := t.SendImage(ctx, room, images[0]); err != nil {
			return fmt.Errorf("send image: %w", err)
		}

		return nil
	}

	if t == nil {
		return errors.New("send images: transport is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	emission := issueReplyEmission(sendCtx)
	threadID, _ := ThreadIDFromContext(sendCtx)

	if err := t.recordReply(sendCtx, room, emission, &StoredReply{Kind: StoredReplyKindImages, Images: images, ThreadID: threadID}); err != nil {
		return fmt.Errorf("record image replies: %w", err)
	}

	return nil
}

// SendError는 message_strings error namespace 문구로 응답한다. 응답 formatter가 없으면 코드 대체 문구를 보내지 않고
// 조립 결함으로 오류를 돌려준다.
func (t *CommandTransport) SendError(ctx context.Context, room, key string) error {
	if t == nil || t.formatter == nil {
		return errors.New("send error message: response formatter is not configured")
	}

	message := t.formatter.ResolveError(ctx, key)
	if message == "" {
		// 기동 검증을 거친 messaging.Err* key에서는 일어나지 않는다. 빈 문구를 보내지 않고 조립 결함으로 드러낸다.
		return fmt.Errorf("send error message: message_strings error/%s has no text", key)
	}

	if err := t.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send error message: %w", err)
	}

	return nil
}
