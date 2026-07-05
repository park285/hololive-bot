package render

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	calendarDiskCacheVersion  = "v4"
	calendarDiskCacheMaxBytes = 8 << 20
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func (r *CalendarCardRenderer) diskCachedImage(key calendarCacheKey) ([]byte, bool) {
	r.diskMu.Lock()
	defer r.diskMu.Unlock()

	path := r.diskCachePath(key)
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > calendarDiskCacheMaxBytes {
		return nil, false
	}
	data, err := readCalendarDiskCacheFile(path)
	if err != nil || !isPNGData(data) {
		return nil, false
	}
	return data, true
}

func (r *CalendarCardRenderer) storeDiskCachedImage(key calendarCacheKey, data []byte) {
	if !isPNGData(data) || len(data) > calendarDiskCacheMaxBytes {
		return
	}

	r.diskMu.Lock()
	defer r.diskMu.Unlock()

	path := r.diskCachePath(key)
	if path == "" || !writeCalendarDiskCacheFile(path, data) {
		return
	}
	r.pruneDiskCacheMonth(key)
}

func writeCalendarDiskCacheFile(path string, data []byte) bool {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return false
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false
	}
	tmpName := tmp.Name()

	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		removeCalendarDiskCacheFile(tmpName)
		return false
	}
	if err := os.Rename(tmpName, path); err != nil {
		removeCalendarDiskCacheFile(tmpName)
		return false
	}
	return true
}

func (r *CalendarCardRenderer) pruneDiskCacheMonth(key calendarCacheKey) {
	keep := r.diskCachePath(key)
	if keep == "" {
		return
	}
	pattern := filepath.Join(filepath.Dir(keep), fmt.Sprintf("%04d-%02d-*.png", key.year, key.month))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, match := range matches {
		if match != keep {
			removeCalendarDiskCacheFile(match)
		}
	}
}

func removeCalendarDiskCacheFile(path string) {
	if err := os.Remove(path); err != nil {
		return
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

func readCalendarDiskCacheFile(path string) ([]byte, error) {
	cleaned := filepath.Clean(path)
	dir, name := filepath.Split(cleaned)
	if dir == "" || name == "" || !fs.ValidPath(name) {
		return nil, fmt.Errorf("invalid calendar disk cache path")
	}
	return fs.ReadFile(os.DirFS(dir), name)
}
