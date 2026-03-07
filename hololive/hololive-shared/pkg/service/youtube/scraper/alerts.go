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

	var foundAlert bool
	var selectedText string
	var selectedNotFound bool

	alerts.ForEach(func(_, alert gjson.Result) bool {
		alertType := alert.Get("alertRenderer.type").String()
		alertText := extractAlertText(alert)

		if alertType == "ERROR" {
			foundAlert = true
			if selectedText == "" {
				selectedText = alertText
			}
			lowerText := strings.ToLower(alertText)
			if strings.Contains(lowerText, "does not exist") ||
				strings.Contains(lowerText, "doesn't exist") ||
				strings.Contains(lowerText, "been terminated") {
				selectedNotFound = true
				if alertText != "" {
					selectedText = alertText
				}
				return false
			}
		}
		return true
	})

	if foundAlert {
		if selectedText == "" {
			selectedText = "unknown channel alert"
		}
		if selectedNotFound {
			return fmt.Errorf("%w: %s", ErrChannelNotFound, selectedText)
		}
		return fmt.Errorf("%w: %s", ErrChannelUnavailable, selectedText)
	}

	return nil
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
