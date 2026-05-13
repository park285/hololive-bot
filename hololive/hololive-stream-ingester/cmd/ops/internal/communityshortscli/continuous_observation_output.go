package communityshortscli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops/communityshorts"
)

func renderContinuousObservationOutput(
	report opsapp.CommunityShortsContinuousObservationReport,
	format string,
) ([]byte, string, error) {
	switch format {
	case "markdown":
		return []byte(opsapp.RenderCommunityShortsContinuousObservationMarkdown(report)), ".md", nil
	case "json":
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, "", err
		}
		payload = append(payload, '\n')
		return payload, ".json", nil
	default:
		return nil, "", fmt.Errorf("unsupported format %q", format)
	}
}

func writeContinuousObservationSnapshot(
	dir string,
	ext string,
	report opsapp.CommunityShortsContinuousObservationReport,
	payload []byte,
) (continuousObservationOutputPaths, error) {
	timestamp := report.GeneratedAt.UTC().Format("20060102-150405")
	snapshotPath := filepath.Join(dir, fmt.Sprintf("snapshot-%s%s", timestamp, ext))
	latestPath := filepath.Join(dir, fmt.Sprintf("latest%s", ext))
	if err := os.WriteFile(snapshotPath, payload, 0o644); err != nil {
		return continuousObservationOutputPaths{}, err
	}
	if err := os.WriteFile(latestPath, payload, 0o644); err != nil {
		return continuousObservationOutputPaths{}, err
	}
	return continuousObservationOutputPaths{latest: latestPath, snapshot: snapshotPath}, nil
}

func defaultContinuousObservationOutputDir(runtimeName string, cutoverAt time.Time) string {
	sanitizedRuntimeName := strings.TrimSpace(runtimeName)
	if sanitizedRuntimeName == "" {
		sanitizedRuntimeName = "youtube-scraper"
	}
	cutoverLabel := cutoverAt.UTC().Format("20060102T150405Z")
	return filepath.Join("artifacts", "youtube-community-shorts-continuous-observation", sanitizedRuntimeName+"-"+cutoverLabel)
}

func parseContinuousObservationCutover(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, errors.New("observation-cutover is required")
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid observation-cutover %q: %v", raw, err)
	}
	return parsed.UTC(), nil
}
