package template

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"text/template"
	"text/template/parse"
	"time"
)

const (
	// TemplateOutputMaxBytes는 렌더 결과의 상한이자 파생 값 불변식의 기준 크기입니다. 알림 digest payload가 받는 메시지
	// 상한(domain.maxDeliveryDigestMessageBytes, 64KiB)과 같아 이보다 큰 결과는 어느 발송 경로에서도 전달될 수 없습니다.
	templateOutputMaxBytes = 64 * 1024
	// TemplateSourceMaxBytes는 template 본문 상한입니다. 한 action 안의 함수 호출 수는 본문 길이에 비례하므로
	// 단계 확인 없이 실행되는 action 하나의 작업량을 이 상한 × 호출당 상한으로 묶습니다.
	templateSourceMaxBytes = 64 * 1024
	// TemplateMaxSteps는 한 번의 실행에서 허용하는 실행 단계 수입니다. 단계는 출력 쓰기와 계측 지점(목록 시작, 각 action과
	// 제어 구문 직전, template 호출)이며 출력 없는 반복과 재귀도 매 반복 단계를 소모합니다. 기본 seed의 가장 긴 묶음
	// 렌더도 수천 단계 안에 끝나므로 10배 이상의 여유를 둡니다.
	templateMaxSteps = 100_000
	// TemplateMaxCallDepth는 {{template}} 중첩 깊이 상한입니다. 기본 seed는 중첩 호출을 쓰지 않으며, 표준 라이브러리의
	// 100000 깊이 제한까지 재귀하면 goroutine stack이 수백 MB로 자라므로 실행 전에 거절합니다.
	templateMaxCallDepth = 64
	// TemplateExecTimeout은 한 번의 실행에 쓰는 시간 예산이며 단계마다, 즉 action 경계에서 확인합니다.
	// 각 action 안의 함수 호출은 시간 확인 없이 실행됩니다. 각 호출의 할당은 파생 값 불변식으로 입력 상한의
	// 상수배이고 호출 수는 본문 상한으로 제한됩니다. 정상 렌더는 수 ms 안에 끝납니다.
	templateExecTimeout = time.Second
)

// ErrTemplateExecutionLimit는 template 실행이 출력·파생 문자열·단계·시간·호출 깊이 예산을 넘었음을 뜻합니다.
// 예산을 넘긴 실행은 부분 결과 없이 실패합니다.
var ErrTemplateExecutionLimit = errors.New("template execution limit exceeded")

// template 호출 진입·종료 표식은 길이 0인 고유 배열로 구분하며 출력에 아무것도 쓰지 않습니다.
var (
	templateEnterMark = make([]byte, 0, 1)
	templateExitMark  = make([]byte, 0, 1)
)

// parseTemplateBody는 공용 함수로 본문을 파싱한 뒤 모든 정의 template에 실행 예산 계측 지점을 넣습니다.
// Strict이면 없는 map key를 오류로 다룹니다. 반환된 template은 실행 중 변경되지 않아 동시 실행에 공유할 수 있습니다.
func parseTemplateBody(name, body string, strict bool) (*template.Template, error) {
	if len(body) > templateSourceMaxBytes {
		return nil, fmt.Errorf("parse template: %w: body %d bytes exceeds %d", ErrTemplateExecutionLimit, len(body), templateSourceMaxBytes)
	}

	tmpl := template.New(name).Funcs(templateFuncs)

	if strict {
		tmpl = tmpl.Option("missingkey=error")
	}

	tmpl, err := tmpl.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	instrumented := make(map[*parse.Tree]bool)

	for _, associated := range tmpl.Templates() {
		tree := associated.Tree
		if tree == nil || tree.Root == nil || instrumented[tree] {
			continue
		}

		instrumented[tree] = true
		instrumentTemplateList(tree.Root)
	}

	return tmpl, nil
}

// instrumentTemplateList는 목록 시작과 각 action·제어 구문 앞에 빈 쓰기 지점을 넣어
// 출력 없는 반복도 실행 예산을 소모하게 합니다. 빈 쓰기는 출력 바이트를 바꾸지 않습니다.
func instrumentTemplateList(list *parse.ListNode) {
	if list == nil {
		return
	}

	nodes := make([]parse.Node, 0, 2*len(list.Nodes)+1)

	nodes = append(nodes, templateMarkNode(list.Pos, nil))

	for _, node := range list.Nodes {
		switch node := node.(type) {
		case *parse.TextNode:
			nodes = append(nodes, node)

			continue
		case *parse.TemplateNode:
			nodes = append(nodes, templateMarkNode(node.Pos, templateEnterMark), node, templateMarkNode(node.Pos, templateExitMark))

			continue
		case *parse.IfNode:
			instrumentTemplateBranch(&node.BranchNode)
		case *parse.RangeNode:
			instrumentTemplateBranch(&node.BranchNode)
		case *parse.WithNode:
			instrumentTemplateBranch(&node.BranchNode)
		}

		nodes = append(nodes, templateMarkNode(node.Position(), nil), node)
	}

	list.Nodes = nodes
}

func instrumentTemplateBranch(branch *parse.BranchNode) {
	instrumentTemplateList(branch.List)
	instrumentTemplateList(branch.ElseList)
}

func templateMarkNode(pos parse.Pos, mark []byte) *parse.TextNode {
	if mark == nil {
		mark = []byte{}
	}

	return &parse.TextNode{NodeType: parse.NodeText, Pos: pos, Text: mark}
}

func isTemplateMark(p, mark []byte) bool {
	return cap(p) > 0 && &p[:1][0] == &mark[:1][0]
}

// boundedTemplateWriter는 실행 단계마다 취소·단계·시간 예산을 확인하고 출력 상한을 넘는 쓰기를 거절합니다.
// 쓰기 오류는 text/template이 즉시 실행을 중단하고 그대로 돌려줍니다. Action 출력은 fmt가 값 전체를 버퍼에 만든 뒤
// 한 번에 쓰므로 이 writer는 그 버퍼를 제한하지 않습니다. Template이 만든 값은 파생 값 불변식으로, 호출자 데이터는
// 호출자가 크기를 제한합니다.
type boundedTemplateWriter struct {
	buf      bytes.Buffer
	done     <-chan struct{}
	cause    func() error
	deadline time.Time
	steps    int
	depth    int
}

func (w *boundedTemplateWriter) Write(p []byte) (int, error) {
	if err := w.step(); err != nil {
		return 0, err
	}

	if len(p) == 0 {
		return 0, w.trackCallDepth(p)
	}

	if len(p) > templateOutputMaxBytes-w.buf.Len() {
		return 0, fmt.Errorf("%w: output exceeds %d bytes", ErrTemplateExecutionLimit, templateOutputMaxBytes)
	}

	n, err := w.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffer template output: %w", err)
	}

	return n, nil
}

func (w *boundedTemplateWriter) step() error {
	w.steps++
	if w.steps > templateMaxSteps {
		return fmt.Errorf("%w: more than %d steps", ErrTemplateExecutionLimit, templateMaxSteps)
	}

	select {
	case <-w.done:
		return fmt.Errorf("template execution interrupted: %w", w.cause())
	default:
	}

	if !time.Now().Before(w.deadline) {
		return fmt.Errorf("%w: exceeded %s", ErrTemplateExecutionLimit, templateExecTimeout)
	}

	return nil
}

func (w *boundedTemplateWriter) trackCallDepth(p []byte) error {
	switch {
	case isTemplateMark(p, templateEnterMark):
		w.depth++
		if w.depth > templateMaxCallDepth {
			return fmt.Errorf("%w: template call depth exceeds %d", ErrTemplateExecutionLimit, templateMaxCallDepth)
		}
	case isTemplateMark(p, templateExitMark):
		w.depth--
	}

	return nil
}

// executeTemplate는 parseTemplateBody로 만든 template을 호출 goroutine에서 예산 안에 실행합니다.
// 취소, 예산 초과, 실행 오류는 부분 출력 없이 실패하며 별도 goroutine을 남기지 않습니다.
func executeTemplate(ctx context.Context, tmpl *template.Template, data any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	writer := boundedTemplateWriter{done: ctx.Done(), cause: ctx.Err, deadline: time.Now().Add(templateExecTimeout)}
	if err := tmpl.Execute(&writer, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	// 마지막 쓰기 뒤 취소된 실행도 성공으로 내보내지 않습니다.
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return writer.buf.String(), nil
}
