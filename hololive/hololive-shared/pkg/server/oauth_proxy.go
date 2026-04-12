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

package server

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
)

const (
	// AppScheme: 앱 Deep Link 스키마 (Android에서 등록 필요)
	AppScheme = "hololive-app"
	// CallbackPath: 콜백 경로
	CallbackPath = "callback"
)

func BuildOAuthDeepLinkURL(code, state, errorParam, errorDesc string) string {
	baseURL := fmt.Sprintf("%s://%s", AppScheme, CallbackPath)
	params := url.Values{}

	if errorParam != "" {
		params.Set("error", errorParam)
		if errorDesc != "" {
			params.Set("error_description", errorDesc)
		}
	} else if code != "" {
		params.Set("code", code)
		if state != "" {
			params.Set("state", state)
		}
	}

	if len(params) > 0 {
		return baseURL + "?" + params.Encode()
	}
	return baseURL
}

// oauthRedirectTmpl: Deep Link 리디렉트 HTML 템플릿 (XSS 방어를 위해 html/template 사용)
var oauthRedirectTmpl = template.Must(template.New("oauth").Parse(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Hololive App - OAuth</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            background: linear-gradient(135deg, {{.Color}} 0%, #764ba2 100%);
        }
        .container {
            text-align: center;
            color: #fff;
            padding: 2rem;
            max-width: 400px;
        }
        .icon { font-size: 64px; margin-bottom: 20px; }
        h1 { margin-bottom: 16px; font-size: 24px; }
        p { opacity: 0.9; margin-bottom: 24px; line-height: 1.6; }
        .button {
            display: inline-block;
            padding: 12px 32px;
            background: rgba(255,255,255,0.2);
            border: 2px solid rgba(255,255,255,0.5);
            border-radius: 8px;
            color: #fff;
            text-decoration: none;
            font-weight: 600;
            transition: all 0.2s;
        }
        .button:hover {
            background: rgba(255,255,255,0.3);
        }
        .help {
            margin-top: 32px;
            padding-top: 16px;
            border-top: 1px solid rgba(255,255,255,0.2);
            font-size: 14px;
            opacity: 0.7;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">{{.Icon}}</div>
        <h1>{{.Status}}</h1>
        <p>앱이 자동으로 열리지 않으면 아래 버튼을 눌러주세요.</p>
        <a href="{{.DeepLinkURL}}" class="button" id="openApp">앱 열기</a>
        <div class="help">
            <p>문제가 계속되면 앱을 다시 설치해주세요.</p>
        </div>
    </div>
    <script>
        window.location.href = {{.DeepLinkURL}};
        setTimeout(function() {
            document.getElementById('openApp').style.background = 'rgba(255,255,255,0.4)';
            document.getElementById('openApp').style.transform = 'scale(1.05)';
        }, 3000);
    </script>
</body>
</html>`))

type oauthRedirectData struct {
	Color       string
	Icon        string
	Status      string
	DeepLinkURL string
}

func BuildOAuthRedirectHTML(deepLinkURL string, isError bool) string {
	data := oauthRedirectData{
		Color:       "#667eea",
		Icon:        "⏳",
		Status:      "로그인 처리 중...",
		DeepLinkURL: deepLinkURL,
	}

	if isError {
		data.Color = "#e74c3c"
		data.Icon = "❌"
		data.Status = "로그인 실패"
	}

	var buf bytes.Buffer
	if err := oauthRedirectTmpl.Execute(&buf, data); err != nil {
		return "<!DOCTYPE html><html><body><p>렌더링 오류</p></body></html>"
	}
	return buf.String()
}
