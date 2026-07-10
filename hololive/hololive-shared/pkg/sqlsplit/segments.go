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

package sqlsplit

import (
	"fmt"
	"strings"
)

type Segment struct {
	Transactional bool
	Statements    []string
}

// Segments는 statement 분할에 top-level BEGIN;/COMMIT; 블록 경계를 더한다.
//
// statement별 autocommit Exec 러너(pgxpool)는 BEGIN을 보내면 그 커넥션이
// 트랜잭션 상태(TxStatus != 'I')로 release돼 pgxpool이 즉시 파기한다 — BEGIN이
// 침묵 해체되고 내부 문장이 각각 autocommit되며 COMMIT은 WARNING으로 성공
// 처리된다. 따라서 러너는 BEGIN/COMMIT 토큰을 직접 실행하면 안 되고, 이 함수가
// 반환하는 Transactional segment를 단일 커넥션의 실제 트랜잭션으로 감싸야 한다.
// BEGIN/COMMIT 토큰 자체는 segment에 포함되지 않는다.
func Segments(sql string) ([]Segment, error) {
	parser := segmentParser{}
	for _, stmt := range Statements(sql) {
		if err := parser.add(stmt); err != nil {
			return nil, err
		}
	}
	return parser.finish()
}

type segmentParser struct {
	segments []Segment
	pending  []string
	inTx     bool
}

func (p *segmentParser) add(stmt string) error {
	control, err := classifyTxControl(stmt)
	if err != nil {
		return err
	}
	switch control {
	case txControlBegin:
		return p.begin()
	case txControlCommit:
		return p.commit()
	case txControlNone:
		return p.appendStatement(stmt)
	}
	return nil
}

func (p *segmentParser) begin() error {
	if p.inTx {
		return fmt.Errorf("nested BEGIN inside a BEGIN/COMMIT block")
	}
	p.flush(false)
	p.inTx = true
	return nil
}

func (p *segmentParser) commit() error {
	if !p.inTx {
		return fmt.Errorf("COMMIT without a matching top-level BEGIN")
	}
	p.flush(true)
	p.inTx = false
	return nil
}

func (p *segmentParser) appendStatement(stmt string) error {
	if p.inTx && containsSQLWord(stmt, "CONCURRENTLY") {
		return fmt.Errorf("CONCURRENTLY statement inside a BEGIN/COMMIT block")
	}
	p.pending = append(p.pending, stmt)
	return nil
}

func (p *segmentParser) finish() ([]Segment, error) {
	if p.inTx {
		return nil, fmt.Errorf("top-level BEGIN without a matching COMMIT")
	}
	p.flush(false)
	return p.segments, nil
}

func (p *segmentParser) flush(transactional bool) {
	if len(p.pending) == 0 {
		return
	}
	p.segments = append(p.segments, Segment{Transactional: transactional, Statements: p.pending})
	p.pending = nil
}

type txControl int

const (
	txControlNone txControl = iota
	txControlBegin
	txControlCommit
)

func classifyTxControl(stmt string) (txControl, error) {
	words := strings.Fields(strings.ToUpper(stripLeadingSQLComments(stmt)))
	if len(words) == 0 {
		return txControlNone, nil
	}
	return classifyTxWords(words)
}

func classifyTxWords(words []string) (txControl, error) {
	switch words[0] {
	case "BEGIN":
		return classifyTxKeywordTail(txControlBegin, words)
	case "START":
		return classifyStartTransaction(words)
	case "COMMIT", "END":
		return classifyTxKeywordTail(txControlCommit, words)
	default:
		return rejectUnsupportedTxControl(words[0])
	}
}

func classifyStartTransaction(words []string) (txControl, error) {
	if len(words) >= 2 && words[1] == "TRANSACTION" {
		return classifyTxKeywordTail(txControlBegin, words[1:])
	}
	return txControlNone, nil
}

func rejectUnsupportedTxControl(keyword string) (txControl, error) {
	switch keyword {
	case "ROLLBACK", "SAVEPOINT", "RELEASE", "ABORT":
		return txControlNone, fmt.Errorf("unsupported top-level transaction control statement %q", keyword)
	default:
		return txControlNone, nil
	}
}

func classifyTxKeywordTail(control txControl, words []string) (txControl, error) {
	for _, word := range words[1:] {
		if word != "WORK" && word != "TRANSACTION" {
			return txControlNone, fmt.Errorf("unsupported transaction control statement %q (only bare BEGIN/COMMIT forms are replayable)", strings.Join(words, " "))
		}
	}
	return control, nil
}

func stripLeadingSQLComments(stmt string) string {
	s := stmt
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if !hasLeadingSQLComment(s) {
			return s
		}
		rest, ok := stripLeadingSQLComment(s)
		if !ok {
			return ""
		}
		s = rest
	}
}

func hasLeadingSQLComment(s string) bool {
	return strings.HasPrefix(s, "--") || strings.HasPrefix(s, "/*")
}

func stripLeadingSQLComment(s string) (string, bool) {
	if strings.HasPrefix(s, "--") {
		return stripLineComment(s)
	}
	return skipNestedBlockComment(s)
}

func stripLineComment(s string) (string, bool) {
	_, after, ok := strings.Cut(s, "\n")
	if !ok {
		return "", false
	}
	return after, true
}

func skipNestedBlockComment(s string) (rest string, ok bool) {
	depth := 0
	i := 0
	for i < len(s) {
		var closed bool
		depth, i, closed = advanceNestedBlockComment(s, i, depth)
		if closed {
			return s[i:], true
		}
	}
	return "", false
}

func advanceNestedBlockComment(s string, pos, depth int) (nextDepth, nextPos int, closed bool) {
	switch {
	case strings.HasPrefix(s[pos:], "/*"):
		return depth + 1, pos + 2, false
	case strings.HasPrefix(s[pos:], "*/"):
		depth--
		return depth, pos + 2, depth == 0
	default:
		return depth, pos + 1, false
	}
}
