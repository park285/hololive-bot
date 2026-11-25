package transport

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/v6/iris"
)

const ReplyPhase = replyPhaseReply

type StoredReplyKind string

const (
	StoredReplyKindText     StoredReplyKind = "text"
	StoredReplyKindRichText StoredReplyKind = "richtext"
	StoredReplyKindImage    StoredReplyKind = "image"
	StoredReplyKindImages   StoredReplyKind = "images"
	StoredReplyKindTextFile StoredReplyKind = "text_file"
)

var (
	ErrStoredReplyInvalid = errors.New("stored reply is invalid")
	ErrReplyStagingFailed = errors.New("reply staging failed")
)

type ReplyOutboxEntry struct {
	MessageID       string
	Phase           string
	Ordinal         uint64
	Room            string
	Payload         string
	ClientRequestID string
}

type ReplyClient interface {
	iris.FileSender
	SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendRichText(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendImage(context.Context, string, []byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
	SendMultipleImages(context.Context, string, [][]byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error)
}

func DispatchStoredReply(ctx context.Context, client ReplyClient, room string, payload []byte, clientRequestID string) (string, error) {
	if client == nil {
		return "", errors.New("dispatch stored reply: iris client is nil")
	}

	var reply StoredReply

	if err := jsonv2.Unmarshal(payload, &reply); err != nil {
		return "", fmt.Errorf("%w: decode: %w", ErrStoredReplyInvalid, err)
	}

	opts := storedReplyOptions(&reply)

	accepted, err := postReplyWithReissue(ctx, clientRequestID, func(generationID string) (*iris.ReplyAcceptedResponse, error) {
		return postStoredReply(ctx, client, room, &reply, appendReplyClientRequestID(opts, generationID))
	})
	if err != nil {
		return "", fmt.Errorf("classify admission error: %w", classifyAdmissionError(err))
	}

	requestID, err := acceptedReplyRequestID(accepted)
	if err != nil {
		return "", fmt.Errorf("accepted reply request id: %w", err)
	}

	return requestID, nil
}

func storedReplyOptions(reply *StoredReply) []iris.SendOption {
	opts := make([]iris.SendOption, 0, 2)

	if reply.ThreadID != "" {
		opts = append(opts, iris.WithThreadID(reply.ThreadID))
	}

	if reply.ImageContentType != "" {
		opts = append(opts, iris.WithImageContentType(reply.ImageContentType))
	}

	return opts
}

func acceptedReplyRequestID(accepted *iris.ReplyAcceptedResponse) (string, error) {
	if accepted == nil {
		return "", replyOutcomeUnknownError{reason: "iris returned no admission response"}
	}

	requestID := strings.TrimSpace(accepted.RequestID)
	if requestID == "" {
		return "", replyOutcomeUnknownError{reason: "iris admission response carried no request id"}
	}

	return requestID, nil
}

func postStoredReply(ctx context.Context, client ReplyClient, room string, reply *StoredReply, opts []iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	if reply == nil {
		return nil, fmt.Errorf("%w: reply is nil", ErrStoredReplyInvalid)
	}

	senders := map[StoredReplyKind]func() (*iris.ReplyAcceptedResponse, error){
		StoredReplyKindTextFile: func() (*iris.ReplyAcceptedResponse, error) {
			file := iris.NewReplyFile(reply.FileName, "text/plain", int64(len(reply.Message)), strings.NewReader(reply.Message))
			return client.SendFile(ctx, room, file, opts...)
		},
		StoredReplyKindRichText: func() (*iris.ReplyAcceptedResponse, error) {
			return client.SendRichText(ctx, room, reply.Message, opts...)
		},
		StoredReplyKindImage: func() (*iris.ReplyAcceptedResponse, error) { return client.SendImage(ctx, room, reply.Image, opts...) },
		StoredReplyKindImages: func() (*iris.ReplyAcceptedResponse, error) {
			return client.SendMultipleImages(ctx, room, reply.Images, opts...)
		},
	}

	if reply.Kind == StoredReplyKindText {
		out, err := client.SendMessageAccepted(ctx, room, reply.Message, opts...)
		if err != nil {
			return nil, fmt.Errorf("send message accepted: %w", err)
		}

		return out, nil
	}

	send, ok := senders[reply.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: unsupported kind %q", ErrStoredReplyInvalid, reply.Kind)
	}

	out, err := send()
	if err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	return out, nil
}

type StoredReply struct {
	FileName         string          `json:"file_name,omitempty"`
	Kind             StoredReplyKind `json:"kind"`
	Message          string          `json:"message,omitempty"`
	Image            []byte          `json:"image,omitempty"`
	Images           [][]byte        `json:"images,omitempty"`
	ThreadID         string          `json:"thread_id,omitempty"`
	ImageContentType string          `json:"image_content_type,omitempty"`
}

type ReplyOutboxWriter interface {
	RecordReply(ctx context.Context, entry *ReplyOutboxEntry) error
}

func ReplyClientRequestID(identity string, ordinal uint64) string {
	return replyClientRequestID(identity, ordinal)
}

func encodeStoredReply(reply *StoredReply) (string, error) {
	payload, err := jsonv2.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	return string(payload), nil
}

func (t *CommandTransport) recordReply(ctx context.Context, room string, emission replyEmission, reply *StoredReply) error {
	writer := t.replyOutbox
	if writer == nil {
		return errors.New("reply outbox writer is not configured")
	}

	if !emission.ok {
		return errors.New("reply identity is not configured")
	}

	payload, err := encodeStoredReply(reply)
	if err != nil {
		return fmt.Errorf("encode stored reply: %w", err)
	}

	if err := writer.RecordReply(ctx, &ReplyOutboxEntry{
		MessageID: emission.identity, Phase: ReplyPhase, Ordinal: emission.ordinal, Room: room,
		Payload: payload, ClientRequestID: emission.clientRequestID,
	}); err != nil {
		return fmt.Errorf("%w: %w", ErrReplyStagingFailed, err)
	}

	return nil
}
