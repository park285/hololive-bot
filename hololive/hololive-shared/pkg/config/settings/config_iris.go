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

package settings

import "time"

// IrisConfig는 Iris egress client의 typed 입력이다. SDK(iris-client-go v4)는 IRIS_* env를 읽지 않으므로
// 이 값이 client 옵션(WithBaseURL·WithBotToken·WithTransport·WithH3CACertFile·WithH3ServerName)의 유일한 원천이다.
type IrisConfig struct {
	BaseURL      string
	BaseURLFile  string
	WebhookToken string // env: IRIS_WEBHOOK_TOKEN
	BotToken     string // env: IRIS_BOT_TOKEN
	HTTPTimeout  time.Duration
	// Transport는 iris.ParseTransport로 검증한 h3 또는 http1이다. env를 비우면 SDK 기본인 h3이다.
	Transport    string // env: IRIS_TRANSPORT
	H3CACertFile string // env: IRIS_H3_CA_CERT_FILE
	H3ServerName string // env: IRIS_H3_SERVER_NAME
	// BaseURLAllowedHosts는 IRIS_BASE_URL_FILE URL host allowlist다. H3ServerName과 함께 허용 집합을 이룬다.
	BaseURLAllowedHosts []string // env: IRIS_BASE_URL_ALLOWED_HOSTS (쉼표 구분)
	// ValidateBaseURLFileStat은 APP_ENV=production일 때 참이며 URL 파일의 경로·소유·권한을 항상 검사한다.
	ValidateBaseURLFileStat bool
}
