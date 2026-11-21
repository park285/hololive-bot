package geminidecision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"google.golang.org/genai/interactions/models/decisions"
	"google.golang.org/genai/interactions/models/interactions"
)

type Request struct {
	Input         decisions.Input
	Questions     map[string]decisions.Question
	QuestionOrder []string
	ChoiceOrder   map[string][]string
}

func (r Request) memberOrder(path jsontext.Pointer) []string {
	switch {
	case path == "":
		return []string{"model", "input", "questions"}
	case path == "/input", path.Parent() == "/input":
		return []string{"type", "text", "mime_type", "data", "resolution"}
	case path == "/questions":
		return r.QuestionOrder
	case path.Parent() == "/questions":
		return []string{"type", "instructions", "criteria"}
	case path.LastToken() == "criteria" && path.Parent().Parent() == "/questions":
		key := path.Parent().LastToken()
		if r.Questions[key].NoulQuestion != nil {
			return []string{"true_description", "false_description"}
		}

		return r.ChoiceOrder[key]
	default:
		return nil
	}
}

func (r Request) writeObject(enc *jsontext.Encoder, members map[string]any) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}

	first := r.memberOrder(enc.StackPointer())
	remaining := make([]string, 0, len(members))

	for name := range members {
		if !slices.Contains(first, name) {
			remaining = append(remaining, name)
		}
	}

	slices.Sort(remaining)

	for _, name := range slices.Concat(first, remaining) {
		value, found := members[name]
		if !found {
			continue
		}

		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return err
		}

		if err := jsonv2.MarshalEncode(enc, value); err != nil {
			return err
		}
	}

	return enc.WriteToken(jsontext.EndObject)
}

var preserveScalars = jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, value *any) error {
	if kind := dec.PeekKind(); kind == jsontext.KindBeginObject || kind == jsontext.KindBeginArray {
		return errors.ErrUnsupported
	}

	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}

	*value = raw.Clone()

	return nil
}))

func (r Request) prepareHTTPRequest(ctx context.Context, req *http.Request) (*http.Request, error) {
	defer req.Body.Close()

	var members map[string]any

	if err := jsonv2.UnmarshalRead(req.Body, &members, preserveScalars); err != nil {
		return nil, fmt.Errorf("read SDK decision request: %w", err)
	}

	raw, err := jsonv2.Marshal(members, jsonv2.WithMarshalers(jsonv2.MarshalToFunc(r.writeObject)))
	if err != nil {
		return nil, fmt.Errorf("order SDK decision request: %w", err)
	}

	out := req.Clone(ctx)

	out.Body = io.NopCloser(bytes.NewReader(raw))
	out.ContentLength = int64(len(raw))
	out.GetBody = nil

	return out, nil
}

func Text(text string) decisions.DecisionInputContent {
	return decisions.NewDecisionInputContent(interactions.TextContent{Text: text})
}

func Image(mime string, data []byte, resolution *interactions.MediaResolution) decisions.DecisionInputContent {
	return decisions.NewDecisionInputContent(interactions.ImageContent{
		MimeType: new(interactions.ImageContentMimeType(mime)), Data: new(base64.StdEncoding.EncodeToString(data)), Resolution: resolution,
	})
}

func ChoiceQuestion[T ~string](instructions string, choices map[T]string) decisions.Question {
	criteria := make(map[string]string, len(choices))
	for choice, description := range choices {
		criteria[string(choice)] = description
	}

	return decisions.NewQuestion(decisions.ChoiceQuestion{Instructions: decisions.NewChoiceQuestionInstructions(instructions), Criteria: criteria})
}

func NoulQuestion(instructions, trueDescription, falseDescription string) decisions.Question {
	return decisions.NewQuestion(decisions.NoulQuestion{
		Instructions: decisions.NewNoulQuestionInstructions(instructions),
		Criteria:     &decisions.NoulCriteria{TrueDescription: &trueDescription, FalseDescription: &falseDescription},
	})
}
