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

package dbtest

import "github.com/jackc/pgx/v5"

// exec_mode는 파라미터 인코딩의 결정 주체(서버 describe 결과 vs 클라이언트의 Go 타입 추론)를
// 바꾼다. 테스트 pool이 운영 compose의 POSTGRES_QUERY_EXEC_MODE 기본값과 다른 모드로 돌면
// []byte→jsonb 같은 특정 모드 전용 인코딩 실패를 테스트가 그대로 통과시킨다.
var productionQueryExecMode = pgx.QueryExecModeCacheStatement

const productionComposeQueryExecModeName = "cache_statement"
