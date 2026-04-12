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

package delivery

import "github.com/kapu/hololive-shared/pkg/domain"

const (
	// OutboxPayloadVersionV1: 현재 outbox payload 버전 (payload 내에 version 필드가 포함되지는 않음)
	OutboxPayloadVersionV1 uint8 = 1
)

const (
	// Kind*: 도메인 값과 동일한 outbox kind (string contract)
	KindMajorEventWeekly  domain.DeliveryOutboxKind = domain.DeliveryKindMajorEventWeekly
	KindMajorEventMonthly domain.DeliveryOutboxKind = domain.DeliveryKindMajorEventMonthly
	KindMemberNewsWeekly  domain.DeliveryOutboxKind = domain.DeliveryKindMemberNewsWeekly
	KindMemberNewsMonthly domain.DeliveryOutboxKind = domain.DeliveryKindMemberNewsMonthly
)

const (
	StatusPending domain.DeliveryOutboxStatus = domain.DeliveryStatusPending
	StatusSent    domain.DeliveryOutboxStatus = domain.DeliveryStatusSent
	StatusFailed  domain.DeliveryOutboxStatus = domain.DeliveryStatusFailed
)

//
// 현재 구현(hololive-shared/pkg/service/delivery/outbox_repository.go)의 JSON({ "message": "..." })과 동일합니다.
type OutboxPayloadV1 struct {
	Message string `json:"message"`
}

//
// 현재 구현은 periodKey + ":" + roomID 입니다.
func ContentID(periodKey, roomID string) string {
	return periodKey + ":" + roomID
}
