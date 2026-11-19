package sqlsplit

import (
	"errors"
	"fmt"
	"strings"
)

type Segment struct {
	Transactional bool
	Statements    []string
}

func Segments(sql string) ([]Segment, error) {
	parser := segmentParser{}

	for _, stmt := range Statements(sql) {
		if err := parser.add(stmt); err != nil {
			return nil, fmt.Errorf("add: %w", err)
		}
	}

	out, err := parser.finish()
	if err != nil {
		return out, fmt.Errorf("finish: %w", err)
	}

	return out, nil
}

type segmentParser struct {
	segments []Segment
	pending  []string
	inTx     bool
}

func (p *segmentParser) add(stmt string) error {
	control, err := classifyTxControl(stmt)
	if err != nil {
		return fmt.Errorf("classify tx control: %w", err)
	}

	switch control {
	case txControlBegin:
		return p.addBegin()
	case txControlCommit:
		return p.addCommit()
	case txControlNone:
		return p.addStatement(stmt)
	}

	return nil
}

func (p *segmentParser) addBegin() error {
	if err := p.begin(); err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	return nil
}

func (p *segmentParser) addCommit() error {
	if err := p.commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (p *segmentParser) addStatement(stmt string) error {
	if err := p.appendStatement(stmt); err != nil {
		return fmt.Errorf("append statement: %w", err)
	}

	return nil
}

func (p *segmentParser) begin() error {
	if p.inTx {
		return errors.New("nested BEGIN inside a BEGIN/COMMIT block")
	}

	p.flush(false)

	p.inTx = true

	return nil
}

func (p *segmentParser) commit() error {
	if !p.inTx {
		return errors.New("COMMIT without a matching top-level BEGIN")
	}

	p.flush(true)

	p.inTx = false

	return nil
}

func (p *segmentParser) appendStatement(stmt string) error {
	if p.inTx && containsSQLWord(stmt, "CONCURRENTLY") {
		return errors.New("CONCURRENTLY statement inside a BEGIN/COMMIT block")
	}

	p.pending = append(p.pending, stmt)

	return nil
}

func (p *segmentParser) finish() ([]Segment, error) {
	if p.inTx {
		return nil, errors.New("top-level BEGIN without a matching COMMIT")
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
	code := stripLeadingSQLComments(stmt)
	end := 0

	for end < len(code) && (code[end] >= 'a' && code[end] <= 'z' || code[end] >= 'A' && code[end] <= 'Z') {
		end++
	}

	switch strings.ToUpper(code[:end]) {
	case "PREPARE":
		return txControlNone, errors.New("top-level PREPARE is not supported in a migration")
	case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "SAVEPOINT", "RELEASE", "ABORT", "SET":
	default:
		return txControlNone, nil
	}

	words := strings.Fields(strings.ToUpper(StripComments(code)))

	out, err := classifyTxWords(words)
	if err != nil {
		return out, fmt.Errorf("classify tx words: %w", err)
	}

	return out, nil
}

func classifyTxWords(words []string) (txControl, error) {
	switch words[0] {
	case "SET":
		if len(words) >= 3 && words[1] == "CONSTRAINTS" && words[2] == "ALL" {
			return txControlNone, errors.New("name the constraint instead of changing transaction-wide constraint timing")
		}

		return txControlNone, nil
	case "BEGIN":
		out, err := classifyTxWordsTail(txControlBegin, words)

		return out, err
	case "START":
		out, err := classifyStartTxWords(words)

		return out, err
	case "COMMIT", "END":
		out, err := classifyTxWordsTail(txControlCommit, words)

		return out, err
	default:
		err := classifyNonControlWord(words[0])

		return txControlNone, err
	}
}

func classifyNonControlWord(word string) error {
	if err := rejectUnsupportedTxControl(word); err != nil {
		return fmt.Errorf("reject unsupported tx control: %w", err)
	}

	return nil
}

func classifyTxWordsTail(control txControl, words []string) (txControl, error) {
	out, err := classifyTxKeywordTail(control, words)
	if err != nil {
		return out, fmt.Errorf("classify tx keyword tail: %w", err)
	}

	return out, nil
}

func classifyStartTxWords(words []string) (txControl, error) {
	out, err := classifyStartTransaction(words)
	if err != nil {
		return out, fmt.Errorf("classify start transaction: %w", err)
	}

	return out, nil
}

func classifyStartTransaction(words []string) (txControl, error) {
	if len(words) >= 2 && words[1] == "TRANSACTION" {
		out, err := classifyTxKeywordTail(txControlBegin, words[1:])
		if err != nil {
			return out, fmt.Errorf("classify tx keyword tail: %w", err)
		}

		return out, nil
	}

	return txControlNone, nil
}

func rejectUnsupportedTxControl(keyword string) error {
	switch keyword {
	case "ROLLBACK", "SAVEPOINT", "RELEASE", "ABORT":
		return fmt.Errorf("unsupported top-level transaction control statement %q", keyword)
	default:
		return nil
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
