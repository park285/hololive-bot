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
