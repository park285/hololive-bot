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
	templateOutputMaxBytes = 64 * 1024
	templateSourceMaxBytes = 64 * 1024
	templateMaxSteps       = 100_000
	templateMaxCallDepth   = 64
	templateExecTimeout    = time.Second
)

var ErrTemplateExecutionLimit = errors.New("template execution limit exceeded")

var (
	templateEnterMark = make([]byte, 0, 1)
	templateExitMark  = make([]byte, 0, 1)
)

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

func executeTemplate(ctx context.Context, tmpl *template.Template, data any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	writer := boundedTemplateWriter{done: ctx.Done(), cause: ctx.Err, deadline: time.Now().Add(templateExecTimeout)}
	if err := tmpl.Execute(&writer, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return writer.buf.String(), nil
}
