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

// Package subscription: 구독 관련 공유 DTO 타입 정의
package subscription

// SubscribeRequest: 구독 요청 DTO (majorevent / membernews 공통)
type SubscribeRequest struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

// SubscriptionStatusResponse: 구독 상태 응답 DTO
type SubscriptionStatusResponse struct {
	Subscribed bool `json:"subscribed"`
}

// StatusResponse: 하위 호환을 위한 별칭 (deprecated)
type StatusResponse = SubscriptionStatusResponse
