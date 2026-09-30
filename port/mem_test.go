package port

import (
	"bytes"
	"errors"
	"testing"
)

func TestMemPairRoundTrip(t *testing.T) {
	a, b := NewMemPair("a", "b")
	defer a.Close()

	want := []byte{1, 2, 3, 4}
	if err := a.WriteFrame(want); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := b.ReadFrame(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("got %v, want %v", buf[:n], want)
	}
}

func TestMemPairClose(t *testing.T) {
	a, b := NewMemPair("a", "b")
	a.Close()
	if _, err := b.ReadFrame(make([]byte, 8)); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
	if err := b.WriteFrame([]byte{1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}
