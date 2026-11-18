package transport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const CommandTimeout = 10 * time.Second

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

func (t *CommandTransport) WithReplyFormat(ctx context.Context, room string) context.Context {
	format := domain.TemplateFormatPlain

	if t != nil && markdownForRoom(ctx, room, t.markdownReplies, t.rooms) {
		format = domain.TemplateFormatRichText
	}

	return messageformatter.WithReplyFormat(ctx, format)
}

func (t *CommandTransport) SendMessage(ctx context.Context, room, message string) error {
	if t == nil {
		return errors.New("send message: transport is not configured")
	}

	useMarkdown := replyUsesRichText(ctx)
	if !useMarkdown {
		message = kakaoformat.Render(message)
	}

	sendCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()

	return t.recordTextReply(sendCtx, room, message, issueReplyEmission(sendCtx), useMarkdown)
}

func replyUsesRichText(ctx context.Context) bool {
	format, ok := messageformatter.ReplyFormatFromContext(ctx)

	return ok && format == domain.TemplateFormatRichText
}

func (t *CommandTransport) recordTextReply(
	ctx context.Context,
	room, message string,
	emission replyEmission,
	useMarkdown bool,
) error {
	kind := StoredReplyKindText

	if useMarkdown {
		kind = StoredReplyKindRichText
	}

	threadID, _ := ThreadIDFromContext(ctx)
	if err := t.recordReply(ctx, room, emission, &StoredReply{Kind: kind, Message: message, ThreadID: threadID}); err != nil {
		return fmt.Errorf("record reply: %w", err)
	}

	return nil
}

func postReplyWithReissue(
	ctx context.Context,
	clientRequestID string,
	post func(string) (*iris.ReplyAcceptedResponse, error),
) (*iris.ReplyAcceptedResponse, error) {
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

	sendCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()

	emission := issueReplyEmission(sendCtx)
	threadID, _ := ThreadIDFromContext(sendCtx)
	contentType, _ := ImageContentTypeFromContext(sendCtx)

	if err := t.recordReply(sendCtx, room, emission, &StoredReply{Kind: StoredReplyKindImage, Image: imageData, ThreadID: threadID, ImageContentType: contentType}); err != nil {
		return fmt.Errorf("record image reply: %w", err)
	}

	return nil
}

func (t *CommandTransport) SendTextFile(ctx context.Context, room, fileName, text string) error {
	if t == nil {
		return errors.New("send text file: transport is not configured")
	}

	if text == "" || len(text) > 30<<20 {
		return errors.New("send text file: invalid payload size")
	}

	sendCtx, cancel := context.WithTimeout(ctx, CommandTimeout)

	defer cancel()

	threadID, _ := ThreadIDFromContext(sendCtx)
	reply := &StoredReply{Kind: StoredReplyKindTextFile, FileName: fileName, Message: text, ThreadID: threadID}

	if err := t.recordReply(sendCtx, room, issueReplyEmission(sendCtx), reply); err != nil {
		return fmt.Errorf("record text file: %w", err)
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

	sendCtx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()

	emission := issueReplyEmission(sendCtx)
	threadID, _ := ThreadIDFromContext(sendCtx)

	if err := t.recordReply(sendCtx, room, emission, &StoredReply{Kind: StoredReplyKindImages, Images: images, ThreadID: threadID}); err != nil {
		return fmt.Errorf("record image replies: %w", err)
	}

	return nil
}

func (t *CommandTransport) SendError(ctx context.Context, room, key string) error {
	if t == nil || t.formatter == nil {
		return errors.New("send error message: response formatter is not configured")
	}

	message := t.formatter.ResolveError(ctx, key)
	if message == "" {
		return fmt.Errorf("send error message: message_strings error/%s has no text", key)
	}

	if err := t.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send error message: %w", err)
	}

	return nil
}
