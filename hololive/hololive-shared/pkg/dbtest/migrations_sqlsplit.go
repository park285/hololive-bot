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

import "strings"

// splitSQLStatements는 SQL 텍스트를 top-level 세미콜론 기준으로 분할한다.
// dollar-quoted 블록($$ ... $$ 또는 $tag$ ... $tag$), 단일/이중 인용 문자열,
// 라인 주석(--), 블록 주석(/* */) 내부의 세미콜론은 구분자로 취급하지 않는다.
// 공백/주석만 남는 조각은 버린다(빈 Exec 방지).
func splitSQLStatements(sql string) []string {
	var (
		statements []string
		buf        strings.Builder
	)

	flush := func() {
		stmt := strings.TrimSpace(buf.String())
		buf.Reset()
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	runes := []rune(sql)
	for i := 0; i < len(runes); {
		next, isSeparator := scanSQLToken(&buf, runes, i)
		if isSeparator {
			flush()
		}
		i = next
	}

	flush()
	return statements
}

// scanSQLToken은 pos에서 시작하는 한 토큰을 buf에 기록하고 다음 인덱스를 반환한다.
// top-level 세미콜론이면 isSeparator=true(이 경우 buf에는 아무것도 쓰지 않는다).
// 라인/블록 주석, 인용 문자열, dollar-quote는 내부 세미콜론을 구분자로 보지 않는다.
func scanSQLToken(buf *strings.Builder, runes []rune, pos int) (next int, isSeparator bool) {
	if end, ok := scanComment(buf, runes, pos); ok {
		return end, false
	}

	c := runes[pos]
	switch c {
	case '\'', '"':
		return scanQuoted(buf, runes, pos), false
	case '$':
		return scanDollar(buf, runes, pos), false
	case ';':
		return pos + 1, true
	default:
		buf.WriteRune(c)
		return pos + 1, false
	}
}

// scanComment은 pos가 라인(--) 또는 블록(/* */) 주석의 시작이면 그 끝까지 buf에
// 기록하고 (end, true)를 반환한다. 주석이 아니면 ok=false.
func scanComment(buf *strings.Builder, runes []rune, pos int) (end int, ok bool) {
	if isLineCommentStart(runes, pos) {
		return scanLineComment(buf, runes, pos), true
	}
	if isBlockCommentStart(runes, pos) {
		return scanBlockComment(buf, runes, pos), true
	}
	return pos, false
}

func isLineCommentStart(runes []rune, i int) bool {
	return runes[i] == '-' && i+1 < len(runes) && runes[i+1] == '-'
}

func isBlockCommentStart(runes []rune, i int) bool {
	return runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '*'
}

// scanLineComment은 라인 주석을 개행 전까지 그대로 보존하며 통과한다(세미콜론 무시).
func scanLineComment(buf *strings.Builder, runes []rune, i int) int {
	for i < len(runes) && runes[i] != '\n' {
		buf.WriteRune(runes[i])
		i++
	}
	return i
}

// scanBlockComment은 블록 주석을 */ 까지 그대로 통과한다.
func scanBlockComment(buf *strings.Builder, runes []rune, i int) int {
	buf.WriteRune(runes[i])
	buf.WriteRune(runes[i+1])
	i += 2
	for i < len(runes) {
		if runes[i] == '*' && i+1 < len(runes) && runes[i+1] == '/' {
			buf.WriteRune(runes[i])
			buf.WriteRune(runes[i+1])
			return i + 2
		}
		buf.WriteRune(runes[i])
		i++
	}
	return i
}

// scanQuoted는 인용 문자열/식별자를 닫는 동일 인용까지 통과한다. ” 또는 "" 이스케이프를 처리한다.
func scanQuoted(buf *strings.Builder, runes []rune, i int) int {
	quote := runes[i]
	buf.WriteRune(runes[i])
	i++
	for i < len(runes) {
		buf.WriteRune(runes[i])
		if runes[i] != quote {
			i++
			continue
		}
		if i+1 < len(runes) && runes[i+1] == quote {
			buf.WriteRune(runes[i+1])
			i += 2
			continue
		}
		return i + 1
	}
	return i
}

// scanDollar는 dollar-quoting($tag$ ... $tag$, tag는 비거나 식별자)를 처리한다.
// pos가 dollar-quote 태그가 아니면 '$' 한 글자만 통과시킨다.
func scanDollar(buf *strings.Builder, runes []rune, i int) int {
	tag, ok := dollarTag(runes, i)
	if !ok {
		buf.WriteRune(runes[i])
		return i + 1
	}

	buf.WriteString(tag)
	i += len([]rune(tag))
	for i < len(runes) {
		if other, ok2 := dollarTag(runes, i); ok2 && other == tag {
			buf.WriteString(other)
			return i + len([]rune(other))
		}
		buf.WriteRune(runes[i])
		i++
	}
	return i
}

// dollarTag는 pos에서 시작하는 dollar-quote 태그($$ 또는 $tag$)를 인식해 그 문자열을 돌려준다.
// 태그가 아니면 ok=false.
func dollarTag(runes []rune, pos int) (string, bool) {
	if pos >= len(runes) || runes[pos] != '$' {
		return "", false
	}

	for j := pos + 1; j < len(runes); j++ {
		c := runes[j]
		if c == '$' {
			return string(runes[pos : j+1]), true
		}
		if !isDollarTagRune(c) {
			return "", false
		}
	}

	return "", false
}

func isDollarTagRune(c rune) bool {
	isLower := c >= 'a' && c <= 'z'
	isUpper := c >= 'A' && c <= 'Z'
	isDigit := c >= '0' && c <= '9'
	return c == '_' || isLower || isUpper || isDigit
}
