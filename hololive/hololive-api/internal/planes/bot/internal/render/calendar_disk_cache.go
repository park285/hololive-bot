package render

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	calendarDiskCacheVersion  = "v3"
	calendarDiskCacheMaxBytes = 8 << 20
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type diskCachePageStatus int

const (
	diskCachePageOK diskCachePageStatus = iota
	diskCachePageMissing
	diskCachePageInvalid
)

func (r *CalendarCardRenderer) diskCachedImages(key calendarCacheKey) ([][]byte, bool) {
	r.diskMu.Lock()
	defer r.diskMu.Unlock()

	var pages [][]byte
	for page := 1; page <= calendarMaxPages; page++ {
		data, status := r.readDiskCachePage(key, page)
		if status == diskCachePageInvalid {
			return nil, false
		}
		if status == diskCachePageMissing {
			break
		}
		pages = append(pages, data)
	}
	if len(pages) == 0 {
		return nil, false
	}
	return pages, true
}

func (r *CalendarCardRenderer) readDiskCachePage(key calendarCacheKey, page int) ([]byte, diskCachePageStatus) {
	path := r.diskCachePagePath(key, page)
	if path == "" {
		return nil, diskCachePageInvalid
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, diskCachePageMissing
	}
	if info.Size() <= 0 || info.Size() > calendarDiskCacheMaxBytes {
		return nil, diskCachePageInvalid
	}
	data, err := readCalendarDiskCacheFile(path)
	if err != nil || !isPNGData(data) {
		return nil, diskCachePageInvalid
	}
	return data, diskCachePageOK
}

// p1을 마지막에 쓴다(커밋 마커): 읽기는 p1부터 시작하므로, 중간에 중단된 쓰기는
// p1 부재로 전체 미스가 되고 부분 페이지 셋이 유효한 짧은 셋으로 오독되지 않는다.
// diskMu는 같은 월·다른 해시의 store/prune/read 인터리브를 직렬화한다 — 없으면
// 타 키의 prune이 커밋 중인 페이지 뒷부분을 지워 {p1}만 남은 잘린 셋이 유효 히트가 된다.
func (r *CalendarCardRenderer) storeDiskCachedImages(key calendarCacheKey, pages [][]byte) {
	if len(pages) == 0 || len(pages) > calendarMaxPages {
		return
	}

	r.diskMu.Lock()
	defer r.diskMu.Unlock()
	written := make([]string, 0, len(pages))
	for i := len(pages) - 1; i >= 0; i-- {
		path, ok := r.writeDiskCachePage(key, i+1, pages[i])
		if !ok {
			removeCalendarDiskCacheFiles(written)
			return
		}
		written = append(written, path)
	}
	r.pruneDiskCacheMonth(key)
}

func (r *CalendarCardRenderer) writeDiskCachePage(key calendarCacheKey, page int, data []byte) (string, bool) {
	path := r.diskCachePagePath(key, page)
	if path == "" || !isPNGData(data) || !writeCalendarDiskCachePage(path, data) {
		return "", false
	}
	return path, true
}

func removeCalendarDiskCacheFiles(paths []string) {
	for _, path := range paths {
		removeCalendarDiskCacheFile(path)
	}
}

func writeCalendarDiskCachePage(path string, data []byte) bool {
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
	first := r.diskCachePagePath(key, 1)
	if first == "" {
		return
	}
	dir := filepath.Dir(first)
	pattern := filepath.Join(dir, fmt.Sprintf("%04d-%02d-*.png", key.year, key.month))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	keepPrefix := filepath.Join(dir, fmt.Sprintf("%04d-%02d-%s-p", key.year, key.month, key.entriesHash))
	for _, match := range matches {
		if !strings.HasPrefix(match, keepPrefix) {
			removeCalendarDiskCacheFile(match)
		}
	}
}

func removeCalendarDiskCacheFile(path string) {
	if err := os.Remove(path); err != nil {
		return
	}
}

func (r *CalendarCardRenderer) diskCachePagePath(key calendarCacheKey, page int) string {
	if r == nil || r.diskCacheDir == "" {
		return ""
	}
	filename := fmt.Sprintf("%04d-%02d-%s-p%d.png", key.year, key.month, key.entriesHash, page)
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
