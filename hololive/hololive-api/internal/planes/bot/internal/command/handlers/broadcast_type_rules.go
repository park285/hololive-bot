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

package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
)

func mustLoadBroadcastRules(data []byte) broadcastTypeRules {
	var rules broadcastTypeRules
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		panic(fmt.Sprintf("load broadcast type rules: %v", err))
	}
	if err := validateBroadcastRules(&rules); err != nil {
		panic(fmt.Sprintf("validate broadcast type rules: %v", err))
	}
	normalizeBroadcastRules(&rules)
	return rules
}

func validateBroadcastRules(rules *broadcastTypeRules) error {
	if strings.TrimSpace(rules.Version) == "" {
		return fmt.Errorf("missing version")
	}
	if err := validateBroadcastTopics(rules.Topics); err != nil {
		return err
	}
	if err := validateBroadcastRuleSet("title rule", rules.TitleRules); err != nil {
		return err
	}
	return validateBroadcastRuleSet("generic title rule", rules.Generic)
}

func validateBroadcastTopics(topics map[string]BroadcastType) error {
	for topic, typ := range topics {
		if !knownBroadcastType(typ) {
			return fmt.Errorf("topic %q uses unknown type %q", topic, typ)
		}
	}
	return nil
}

func validateBroadcastRuleSet(kind string, rules []broadcastTitleRule) error {
	for _, rule := range rules {
		if !knownBroadcastType(rule.Type) {
			return fmt.Errorf("%s uses unknown type %q", kind, rule.Type)
		}
		if len(rule.Keywords) == 0 {
			return fmt.Errorf("%s %q has no keywords", kind, rule.Type)
		}
	}
	return nil
}

func normalizeBroadcastRules(rules *broadcastTypeRules) {
	topics := make(map[string]BroadcastType, len(rules.Topics))
	for topic, typ := range rules.Topics {
		topics[normalizeBroadcastTopic(topic)] = typ
	}
	rules.Topics = topics
	for i := range rules.TitleRules {
		rules.TitleRules[i].Keywords = normalizeBroadcastKeywords(rules.TitleRules[i].Keywords)
		rules.TitleRules[i].RejectKeywords = normalizeBroadcastKeywords(rules.TitleRules[i].RejectKeywords)
	}
	for i := range rules.Generic {
		rules.Generic[i].Keywords = normalizeBroadcastKeywords(rules.Generic[i].Keywords)
		rules.Generic[i].RejectKeywords = normalizeBroadcastKeywords(rules.Generic[i].RejectKeywords)
	}
	rules.GameTag.RejectKeywords = normalizeBroadcastKeywords(rules.GameTag.RejectKeywords)
	rules.GameTag.Exact = normalizeBroadcastTitleTags(rules.GameTag.Exact)
	rules.GameTag.Contains = normalizeBroadcastKeywords(rules.GameTag.Contains)
}

func normalizeBroadcastKeywords(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeBroadcastText(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeBroadcastTitleTags(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeBroadcastTitleTag(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func knownBroadcastType(typ BroadcastType) bool {
	return broadcasttype.Known(typ)
}
