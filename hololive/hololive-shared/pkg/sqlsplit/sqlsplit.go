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

// Package sqlsplit는 migration SQL을 statement 단위로 분할한다.
//
// pgx simple query protocol이 멀티-statement 문자열을 암묵 트랜잭션 블록으로 감싸
// CREATE INDEX CONCURRENTLY가 "cannot run inside a transaction block"으로 실패하므로,
// 각 statement를 개별 autocommit Exec로 보내야 한다. 분할 계약: 구분자는 top-level
// 세미콜론뿐이며 dollar-quote($$/$tag$)·인용 문자열·주석 내부의 세미콜론은 제외한다.
package sqlsplit

import "strings"

func Statements(sql string) []string {
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

func scanLineComment(buf *strings.Builder, runes []rune, i int) int {
	for i < len(runes) && runes[i] != '\n' {
		buf.WriteRune(runes[i])
		i++
	}
	return i
}

func scanBlockComment(buf *strings.Builder, runes []rune, i int) int {
	depth := 0
	for i < len(runes) {
		next, nextDepth, closed := scanBlockCommentToken(buf, runes, i, depth)
		i = next
		depth = nextDepth
		if closed {
			return i
		}
	}
	return i
}

func scanBlockCommentToken(buf *strings.Builder, runes []rune, pos, depth int) (next, nextDepth int, closed bool) {
	if isBlockCommentStart(runes, pos) {
		writeRunePair(buf, runes, pos)
		return pos + 2, depth + 1, false
	}
	if isBlockCommentEnd(runes, pos) {
		writeRunePair(buf, runes, pos)
		depth--
		return pos + 2, depth, depth == 0
	}
	buf.WriteRune(runes[pos])
	return pos + 1, depth, false
}

func isBlockCommentEnd(runes []rune, pos int) bool {
	return runes[pos] == '*' && pos+1 < len(runes) && runes[pos+1] == '/'
}

func writeRunePair(buf *strings.Builder, runes []rune, pos int) {
	buf.WriteRune(runes[pos])
	buf.WriteRune(runes[pos+1])
}

func scanQuoted(buf *strings.Builder, runes []rune, i int) int {
	quote := runes[i]
	backslashEscapes := isEscapeStringQuote(runes, i)
	buf.WriteRune(runes[i])
	i++
	for i < len(runes) {
		if next, escaped := scanQuotedEscape(buf, runes, i, backslashEscapes); escaped {
			i = next
			continue
		}
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

func scanQuotedEscape(buf *strings.Builder, runes []rune, pos int, enabled bool) (next int, escaped bool) {
	if !enabled || runes[pos] != '\\' || pos+1 >= len(runes) {
		return pos, false
	}
	writeRunePair(buf, runes, pos)
	return pos + 2, true
}

func isEscapeStringQuote(runes []rune, quotePos int) bool {
	if runes[quotePos] != '\'' || quotePos == 0 {
		return false
	}
	prefixPos := quotePos - 1
	if runes[prefixPos] != 'E' && runes[prefixPos] != 'e' {
		return false
	}
	return prefixPos == 0 || !isDollarTagRune(runes[prefixPos-1])
}

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

func containsSQLWord(sql, target string) bool {
	runes := []rune(sql)
	var discard strings.Builder
	for pos := 0; pos < len(runes); {
		if next, skipped := scanNonCodeToken(&discard, runes, pos); skipped {
			discard.Reset()
			pos = next
			continue
		}
		if !isDollarTagRune(runes[pos]) {
			pos++
			continue
		}
		end := scanSQLWordEnd(runes, pos)
		if strings.EqualFold(string(runes[pos:end]), target) {
			return true
		}
		pos = end
	}
	return false
}

func scanNonCodeToken(buf *strings.Builder, runes []rune, pos int) (next int, skipped bool) {
	if end, ok := scanComment(buf, runes, pos); ok {
		return end, true
	}
	switch runes[pos] {
	case '\'', '"':
		return scanQuoted(buf, runes, pos), true
	case '$':
		return scanDollar(buf, runes, pos), true
	default:
		return pos, false
	}
}

func scanSQLWordEnd(runes []rune, pos int) int {
	for pos < len(runes) && isDollarTagRune(runes[pos]) {
		pos++
	}
	return pos
}
