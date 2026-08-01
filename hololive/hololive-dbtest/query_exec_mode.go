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

// 운영 compose(deploy/compose/docker-compose.prod.yml의 POSTGRES_QUERY_EXEC_MODE 기본값)와
// 같은 모드로 테스트 pool을 돌린다. exec는 서버 describe 없이 클라이언트가 Go 타입으로
// 인코딩을 정하므로, pgx 기본 모드로 테스트하면 []byte→jsonb 같은 운영 전용 인코딩 실패가
// 테스트를 통과해 버린다. 기본값 드리프트는 TestProductionComposePinsTheTestPoolQueryExecMode가 막는다.
var productionQueryExecMode = pgx.QueryExecModeExec

const productionComposeQueryExecModeName = "exec"
