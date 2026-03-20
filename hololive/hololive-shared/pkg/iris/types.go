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

package iris

import (
	"park285/iris-client-go/client"
	"park285/iris-client-go/webhook"
)

// 클라이언트 타입 별칭 — iris-client-go/client로 위임합니다.
type (
	ReplyRequest    = client.ReplyRequest
	Config          = client.Config
	DecryptRequest  = client.DecryptRequest
	DecryptResponse = client.DecryptResponse
)

// 웹훅 타입 별칭 — iris-client-go/webhook으로 위임합니다.
type (
	WebhookRequest = webhook.WebhookRequest
	Message        = webhook.Message
	MessageJSON    = webhook.MessageJSON
	MessageHandler = webhook.MessageHandler
	HandlerOption  = webhook.HandlerOption
)
