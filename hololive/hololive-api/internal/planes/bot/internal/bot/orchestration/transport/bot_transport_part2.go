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

	"github.com/park285/iris-client-go/v2/iris"

	appErrors "github.com/kapu/hololive-shared/pkg/apperrors"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func (t *CommandTransport) SendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send image: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if t.replyOutboxWriter() != nil {
		emission := issueReplyEmission(sendCtx)
		threadID, _ := ThreadIDFromContext(sendCtx)
		contentType, _ := ImageContentTypeFromContext(sendCtx)

		if err := t.recordReply(sendCtx, room, emission, &StoredReply{Kind: StoredReplyKindImage, Image: imageData, ThreadID: threadID, ImageContentType: contentType}); err != nil {
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
		if err := t.SendImage(ctx, room, images[0], opts...); err != nil {
			return fmt.Errorf("send image: %w", err)
		}

		return nil
	}

	if err := t.sendMultipleImages(ctx, room, images, opts...); err != nil {
		return fmt.Errorf("send multiple images: %w", err)
	}

	return nil
}

func (t *CommandTransport) sendMultipleImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) error {
	if t == nil || t.irisClient == nil {
		return errors.New("send images: iris client is not configured")
	}

	sendCtx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if t.replyOutboxWriter() != nil {
		emission := issueReplyEmission(sendCtx)
		threadID, _ := ThreadIDFromContext(sendCtx)

		if err := t.recordReply(sendCtx, room, emission, &StoredReply{Kind: StoredReplyKindImages, Images: images, ThreadID: threadID}); err != nil {
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
