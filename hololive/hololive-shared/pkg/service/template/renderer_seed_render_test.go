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

package template

import (
	"bytes"
	"context"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 런타임 renderer는 missingkey 옵션 없이 파싱해 map 데이터의 키 누락이 `<no value>`로
// 조용히 노출된다(struct 필드 누락만 에러). 이 테스트가 명시적 missingkey=error로 전 키를
// 시드 본문 그대로 렌더해, 시드-샘플-변수 계약 위반을 배포 전에 유일하게 차단한다.
func TestSeedTemplates_RenderAllKeysWithSampleData(t *testing.T) {
	pool := dbtest.NewPool(t)

	rows, err := pool.Query(context.Background(),
		`SELECT template_key, body FROM notification_templates WHERE channel_id IS NULL`)
	if err != nil {
		t.Fatalf("query seeds: %v", err)
	}
	defer rows.Close()

	seeds := make(map[string]string)
	for rows.Next() {
		var key, body string
		if err := rows.Scan(&key, &body); err != nil {
			t.Fatalf("scan seed row: %v", err)
		}
		seeds[key] = body
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seed rows: %v", err)
	}

	keys := domain.GetAllTemplateKeys()
	keyset := make(map[string]bool, len(keys))
	for _, key := range keys {
		keyset[string(key)] = true

		body, ok := seeds[string(key)]
		if !ok {
			t.Errorf("%s: 기본 시드 행이 없음", key)
			continue
		}

		data := domain.GetTemplateSampleData(key)
		if data == nil {
			t.Errorf("%s: sample data 없음", key)
			continue
		}

		tmpl, err := texttemplate.New(string(key)).Funcs(templateFuncs).Option("missingkey=error").Parse(body)
		if err != nil {
			t.Errorf("%s: parse 실패: %v", key, err)
			continue
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			t.Errorf("%s: sample data 렌더 실패: %v", key, err)
			continue
		}
		if strings.Contains(buf.String(), "<no value>") {
			t.Errorf("%s: 렌더 결과에 <no value> 노출", key)
		}
	}

	for key := range seeds {
		if !keyset[key] {
			t.Errorf("시드 키 %s가 GetAllTemplateKeys()에 없음 — 렌더 게이트 밖 키 금지", key)
		}
	}
}
