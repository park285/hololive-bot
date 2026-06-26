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

package llm

import (
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3/responses"
)

func extractResponsesOutputTextWithDiagnostics(resp *responses.Response) (string, error) {
	if resp == nil {
		return "", errOpenAIEmptyOutput
	}

	text := strings.TrimSpace(resp.OutputText())
	if text != "" {
		return text, nil
	}

	diagnostic := describeResponsesOutput(resp)
	if diagnostic == "" {
		return "", errOpenAIEmptyOutput
	}

	if responsesOutputHasRefusal(resp.Output) {
		return "", fmt.Errorf("%w: %w: %s", errOpenAIEmptyOutput, errOpenAIRefusalOutput, diagnostic)
	}

	return "", fmt.Errorf("%w: %s", errOpenAIEmptyOutput, diagnostic)
}

func extractResponsesOutputText(resp *responses.Response) (string, error) {
	return extractResponsesOutputTextWithDiagnostics(resp)
}

func describeResponsesOutput(resp *responses.Response) string {
	if resp == nil {
		return ""
	}

	parts := make([]string, 0, 4)
	parts = appendResponseStatusDetails(parts, resp)
	parts = appendResponseOutputDiagnostics(parts, resp.Output)

	return strings.Join(parts, " ")
}

func appendResponseStatusDetails(parts []string, resp *responses.Response) []string {
	if resp.Status != "" {
		parts = append(parts, fmt.Sprintf("status=%s", resp.Status))
	}
	if resp.IncompleteDetails.Reason != "" {
		parts = append(parts, fmt.Sprintf("incomplete_reason=%s", resp.IncompleteDetails.Reason))
	}

	return parts
}

func appendResponseOutputDiagnostics(parts []string, output []responses.ResponseOutputItemUnion) []string {
	outputTypes := make([]string, 0, len(output))
	for i := range output {
		item := &output[i]
		outputTypes = append(outputTypes, describeResponseOutputItemType(item))
		if refusal := responseOutputItemRefusal(item); refusal != "" {
			parts = append(parts, "refusal=true")
		}
	}

	if len(outputTypes) > 0 {
		parts = append(parts, fmt.Sprintf("output=%s", strings.Join(outputTypes, ",")))
	}

	return parts
}

func responsesOutputHasRefusal(output []responses.ResponseOutputItemUnion) bool {
	for i := range output {
		if responseOutputItemRefusal(&output[i]) != "" {
			return true
		}
	}
	return false
}

func describeResponseOutputItemType(item *responses.ResponseOutputItemUnion) string {
	if item == nil {
		return "unknown"
	}
	itemType := strings.TrimSpace(item.Type)
	if itemType == "" {
		itemType = "unknown"
	}
	if item.Status != "" {
		return fmt.Sprintf("%s/%s", itemType, item.Status)
	}
	return itemType
}

func responseOutputItemRefusal(item *responses.ResponseOutputItemUnion) string {
	if item == nil || item.Type != "message" {
		return ""
	}
	for i := range item.Content {
		content := &item.Content[i]
		if content.Type == "refusal" && strings.TrimSpace(content.Refusal) != "" {
			return strings.TrimSpace(content.Refusal)
		}
	}
	return ""
}
