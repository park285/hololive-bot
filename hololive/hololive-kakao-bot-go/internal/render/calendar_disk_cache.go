package render

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

const (
	calendarDiskCacheVersion  = "v1"
	calendarDiskCacheMaxBytes = 8 << 20
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func (r *CalendarCardRenderer) diskCachedImage(key calendarCacheKey) ([]byte, bool) {
	path := r.diskCachePath(key)
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > calendarDiskCacheMaxBytes {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil || !isPNGData(data) {
		return nil, false
	}
	return bytes.Clone(data), true
}

func (r *CalendarCardRenderer) storeDiskCachedImage(key calendarCacheKey, data []byte) {
	if !isPNGData(data) {
		return
	}
	path := r.diskCachePath(key)
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()

	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}

func (r *CalendarCardRenderer) diskCachePath(key calendarCacheKey) string {
	if r == nil || r.diskCacheDir == "" {
		return ""
	}
	filename := fmt.Sprintf("%04d-%02d-%s.png", key.year, key.month, key.entriesHash)
	return filepath.Join(r.diskCacheDir, calendarDiskCacheVersion, filename)
}

func isPNGData(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature)
}
