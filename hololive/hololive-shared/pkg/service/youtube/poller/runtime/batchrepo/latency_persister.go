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

package batchrepo

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// LatencyClassificationIdentity는 post-commit 지연 분류 저장에 필요한 최소 식별 정보입니다.
type LatencyClassificationIdentity struct {
	Kind      domain.OutboxKind
	ContentID string
}

// PostLatencyClassificationPersister는 트랜잭션 커밋 후 post 지연 분류를 저장하는 seam입니다.
// batchrepo가 outbox/delivery 계층에 직접 의존하지 않도록 추상화합니다.
type PostLatencyClassificationPersister interface {
	PersistPostLatencyClassificationsByIdentities(ctx context.Context, identities []LatencyClassificationIdentity) error
}
