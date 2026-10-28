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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/park285/iris-client-go/v3/iris"
)

const ReplyPhase = replyPhaseReply

type StoredReplyKind string

const (
	StoredReplyKindText     StoredReplyKind = "text"
	StoredReplyKindRichText StoredReplyKind = "richtext"
	StoredReplyKindMarkdown StoredReplyKind = "markdown"
	StoredReplyKindImage    StoredReplyKind = "image"
	StoredReplyKindImages   StoredReplyKind = "images"
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

// DispatchStoredReply는 저장된 응답을 Iris에 보내고 durable admission이 증명한 Iris requestId를 돌려준다.
// Handoff 결과는 기다리지 않는다. 그 확인은 outbox의 accepted 행을 읽는 reconciler가 맡는다.
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

// acceptedReplyRequestID는 202 응답이 durable admission을 증명하는지 확인한다. 응답이나 requestId가
// 없으면 admission 결과가 불명이므로 같은 clientRequestId 재확인 대상으로 남긴다.
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
		StoredReplyKindRichText: func() (*iris.ReplyAcceptedResponse, error) {
			return client.SendRichText(ctx, room, reply.Message, opts...)
		},
		// bot reply outbox가 소유하는 기존 markdown 행은 같은 ID의 body·semantic hash를 보존한다.
		// 기존 행이 모두 drain된 뒤에만 이 읽기 경로를 제거하며 richtext로 재해석하지 않는다.
		StoredReplyKindMarkdown: func() (*iris.ReplyAcceptedResponse, error) {
			return client.SendMarkdown(ctx, room, reply.Message, opts...)
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

func encodeStoredReply(reply *StoredReply) (string, error) {
	payload, err := jsonv2.Marshal(reply)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	return string(payload), nil
}

func (t *CommandTransport) recordReply(ctx context.Context, room string, emission replyEmission, reply *StoredReply) error {
	writer := t.replyOutboxWriter()
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
