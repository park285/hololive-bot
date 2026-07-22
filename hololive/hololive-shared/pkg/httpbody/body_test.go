package httpbody

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type trackingBody struct {
	reader   io.Reader
	read     int
	sawEOF   bool
	closed   bool
	closeErr error
}

func (b *trackingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	if errors.Is(err, io.EOF) {
		b.sawEOF = true
	}
	return n, err
}

func (b *trackingBody) Close() error {
	b.closed = true
	return b.closeErr
}

func TestReadAllAndCloseAcceptsExactLimit(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader("12345")}
	got, err := ReadAllAndClose(body, 5)
	if err != nil {
		t.Fatalf("ReadAllAndClose() error = %v", err)
	}
	if string(got) != "12345" {
		t.Fatalf("body = %q, want 12345", got)
	}
	if !body.closed {
		t.Fatal("body was not closed")
	}
}

func TestReadAllAndCloseRejectsOversizedBodyAndDrainsRemainder(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader(strings.Repeat("x", 32))}
	_, err := ReadAllAndClose(body, 8)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
	if body.read != 32 {
		t.Fatalf("bytes read = %d, want 32 (limit probe plus bounded drain)", body.read)
	}
	if !body.closed {
		t.Fatal("body was not closed")
	}
}

func TestReadAllAndClosePreservesCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	body := &trackingBody{reader: strings.NewReader("ok"), closeErr: closeErr}
	_, err := ReadAllAndClose(body, 8)
	if !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want close error", err)
	}
}

func TestDrainAndCloseIsBounded(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader(strings.Repeat("x", 100))}
	if err := DrainAndClose(body, 16); err != nil {
		t.Fatalf("DrainAndClose() error = %v", err)
	}
	if body.read != 17 {
		t.Fatalf("bytes drained = %d, want 17 (limit plus EOF probe)", body.read)
	}
	if !body.closed {
		t.Fatal("body was not closed")
	}
}

func TestDrainAndCloseReachesEOFAtExactLimit(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader(strings.Repeat("x", 16))}
	if err := DrainAndClose(body, 16); err != nil {
		t.Fatalf("DrainAndClose() error = %v", err)
	}
	if !body.sawEOF {
		t.Fatal("exact-limit body did not reach EOF")
	}
}

func TestReadAllAndCloseRejectsNilBody(t *testing.T) {
	if _, err := ReadAllAndClose(nil, 1); !errors.Is(err, ErrNilBody) {
		t.Fatalf("error = %v, want ErrNilBody", err)
	}
}
