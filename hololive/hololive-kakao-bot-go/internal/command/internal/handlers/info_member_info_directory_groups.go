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

package handlers

const defaultMemberDirectoryGroup = "기타"

var memberDirectoryPreferredOrder = []string{
	"Advent",
	"FLOW GLOW",
	"Justice",
	"Myth",
	"Promise",
	"ReGLOSS",
	"비밀결사 holoX",
	"홀로라이브 0기생",
	"홀로라이브 1기생",
	"홀로라이브 2기생",
	"홀로라이브 3기생",
	"홀로라이브 4기생",
	"홀로라이브 5기생",
	"홀로라이브 게이머즈",
	"홀로라이브 인도네시아",
}

var memberDirectoryGroupAliases = map[string]string{
	"秘密結社holoX":                       "비밀결사 holoX",
	"ホロライブ0期生":                        "홀로라이브 0기생",
	"ホロライブ1期生":                        "홀로라이브 1기생",
	"ホロライブ2期生":                        "홀로라이브 2기생",
	"ホロライブ3期生":                        "홀로라이브 3기생",
	"ホロライブ4期生":                        "홀로라이브 4기생",
	"ホロライブ5期生":                        "홀로라이브 5기생",
	"ホロライブゲーマーズ":                      "홀로라이브 게이머즈",
	"ホロライブインドネシア":                     "홀로라이브 인도네시아",
	"ホロライブインドネシア（hololive Indonesia）": "홀로라이브 인도네시아",
	"Myth（神話）":                        "Myth",
	"Promise（約束）":                     "Promise",
	"ホロライブEnglish -Myth-":             "Myth",
	"ホロライブEnglish -Promise-":          "Promise",
	"hololive English Myth":           "Myth",
	"hololive English Promise":        "Promise",
}
