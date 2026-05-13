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

package scraper

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func checkAlerts(data gjson.Result) error {
	alerts := data.Get("alerts")
	if !alerts.Exists() {
		return nil
	}

	selected := selectAlertError(alerts)
	if !selected.found {
		return nil
	}

	if selected.text == "" {
		selected.text = "unknown channel alert"
	}
	if selected.notFound {
		return fmt.Errorf("%w: %s", ErrChannelNotFound, selected.text)
	}
	return fmt.Errorf("%w: %s", ErrChannelUnavailable, selected.text)
}

type selectedAlertError struct {
	found    bool
	text     string
	notFound bool
}

func selectAlertError(alerts gjson.Result) selectedAlertError {
	selected := selectedAlertError{}
	alerts.ForEach(func(_, alert gjson.Result) bool {
		selected = selectAlertCandidate(selected, alert)
		return !selected.notFound
	})
	return selected
}

func selectAlertCandidate(selected selectedAlertError, alert gjson.Result) selectedAlertError {
	if alert.Get("alertRenderer.type").String() != "ERROR" {
		return selected
	}

	alertText := extractAlertText(alert)
	selected.found = true
	if selected.text == "" {
		selected.text = alertText
	}
	return selectNotFoundAlertText(selected, alertText)
}

func selectNotFoundAlertText(selected selectedAlertError, alertText string) selectedAlertError {
	if !alertTextMeansNotFound(alertText) {
		return selected
	}

	selected.notFound = true
	if alertText != "" {
		selected.text = alertText
	}
	return selected
}

func alertTextMeansNotFound(alertText string) bool {
	lowerText := strings.ToLower(alertText)
	return strings.Contains(lowerText, "does not exist") ||
		strings.Contains(lowerText, "doesn't exist") ||
		strings.Contains(lowerText, "been terminated")
}

func extractAlertText(alert gjson.Result) string {
	alertText := strings.TrimSpace(alert.Get("alertRenderer.text.simpleText").String())
	if alertText != "" {
		return alertText
	}

	var parts []string
	alert.Get("alertRenderer.text.runs").ForEach(func(_, run gjson.Result) bool {
		text := strings.TrimSpace(run.Get("text").String())
		if text != "" {
			parts = append(parts, text)
		}
		return true
	})

	return strings.TrimSpace(strings.Join(parts, " "))
}
