package scraper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileSnapshotSink struct {
	Dir string
}

func NewFileSnapshotSink(dir string) FileSnapshotSink {
	return FileSnapshotSink{Dir: dir}
}

func (s FileSnapshotSink) Capture(ctx context.Context, snapshot Snapshot) error {
	if strings.TrimSpace(s.Dir) == "" || snapshot.CapturedAt.IsZero() {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	id := SnapshotID(snapshot)
	date := snapshot.CapturedAt.UTC().Format("20060102")
	name := fmt.Sprintf("%s_%s_%s_%s_%s.html", date, safeFilePart(snapshot.Operation), safeFilePart(snapshot.ChannelID), safeFilePart(snapshot.Stage), id[:12])
	dir := filepath.Join(s.Dir, date)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), snapshot.Body, 0o644)
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(value)
}
