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

// Package dbx: PostgreSQL 연결 공통 모듈
//
// # 개요
//
// dbx는 PostgreSQL 연결을 위한 공통 모듈입니다.
// pgxpool 기반 PostgreSQL 연결을 지원하며, 다음 기능을 제공합니다:
//
//   - DSN 생성 (UDS/TCP 자동 전환)
//   - 커넥션 풀 설정
//   - Exponential backoff 재시도
//   - Ping/Close 헬퍼
//   - pgx 트랜잭션 헬퍼
//   - PostgreSQL 에러 판별
//
// # 사용 예시
//
// 기본 사용:
//
//	config := dbx.Config{
//		Host:     "localhost",
//		Port:     5432,
//		User:     "myuser",
//		Password: "example",
//		Name:     "mydb",
//	}
//
//	client, err := dbx.Open(ctx, config, dbx.DefaultOpenOptions())
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer client.Close()
//
//	// pgxpool 사용 (raw SQL)
//	pool := client.Pool()
//	pool.QueryRow(ctx, "SELECT 1")
//
// Retry 사용:
//
//	client, err := dbx.OpenWithRetry(ctx, config, dbx.DefaultOpenOptions())
//
// 트랜잭션 헬퍼:
//
//	err := dbx.InPgxTx(ctx, client.Pool(), func(tx dbx.Tx) error {
//		_, err := tx.Exec(ctx, "INSERT INTO users (name) VALUES ($1)", user.Name)
//		return err
//	})
//
// 에러 판별:
//
//	if dbx.IsDuplicateKey(err) {
//		// unique constraint 위반 처리
//	}
package dbx
