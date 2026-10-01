package port

import (
	"bytes"
	"errors"
	"testing"
)

// readFrame は 1 フレームを読み、その内容だけを返すテスト用ヘルパー。
func readFrame(t *testing.T, p Port) []byte {
	t.Helper()
	buf := make([]byte, 64)
	n, err := p.ReadFrame(buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	return buf[:n]
}

func TestMemPair_片側に書いたフレームは反対側で読める(t *testing.T) {
	a, b := NewMemPair("a", "b")
	frame := []byte{1, 2, 3, 4}

	if err := a.WriteFrame(frame); err != nil {
		t.Fatal(err)
	}

	if got := readFrame(t, b); !bytes.Equal(got, frame) {
		t.Fatalf("got %v, want %v", got, frame)
	}
}

func TestMemPair_フレームは両方向に独立して流れる(t *testing.T) {
	a, b := NewMemPair("a", "b")
	fromA := []byte{0xaa}
	fromB := []byte{0xbb}

	if err := a.WriteFrame(fromA); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteFrame(fromB); err != nil {
		t.Fatal(err)
	}

	if got := readFrame(t, b); !bytes.Equal(got, fromA) {
		t.Fatalf("b received %v, want %v", got, fromA)
	}
	if got := readFrame(t, a); !bytes.Equal(got, fromB) {
		t.Fatalf("a received %v, want %v", got, fromB)
	}
}

func TestMemPair_フレームは書き込んだ順に届く(t *testing.T) {
	a, b := NewMemPair("a", "b")
	first, second := []byte{1}, []byte{2}

	if err := a.WriteFrame(first); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteFrame(second); err != nil {
		t.Fatal(err)
	}

	if got := readFrame(t, b); !bytes.Equal(got, first) {
		t.Fatalf("1st frame: got %v, want %v", got, first)
	}
	if got := readFrame(t, b); !bytes.Equal(got, second) {
		t.Fatalf("2nd frame: got %v, want %v", got, second)
	}
}

func TestMemPair_書き込み後に元のバッファを変更しても届くフレームは変わらない(t *testing.T) {
	a, b := NewMemPair("a", "b")
	frame := []byte{1, 2, 3}
	if err := a.WriteFrame(frame); err != nil {
		t.Fatal(err)
	}

	frame[0] = 0xff // 送信側がバッファを再利用するケース

	if got := readFrame(t, b); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("got %v, want [1 2 3]", got)
	}
}

func TestMemPair_作成時に指定した名前を返す(t *testing.T) {
	a, b := NewMemPair("nameA", "nameB")

	if a.Name() != "nameA" || b.Name() != "nameB" {
		t.Fatalf("got (%q, %q), want (%q, %q)", a.Name(), b.Name(), "nameA", "nameB")
	}
}

func TestMemPair_相手が閉じられると読み込みはErrClosedで失敗する(t *testing.T) {
	a, b := NewMemPair("a", "b")
	a.Close()

	_, err := b.ReadFrame(make([]byte, 8))

	if !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

func TestMemPair_相手が閉じられると書き込みはErrClosedで失敗する(t *testing.T) {
	a, b := NewMemPair("a", "b")
	a.Close()

	err := b.WriteFrame([]byte{1})

	if !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

func TestMemPair_自分自身を閉じた後は読み書きともErrClosedで失敗する(t *testing.T) {
	a, _ := NewMemPair("a", "b")
	a.Close()

	_, readErr := a.ReadFrame(make([]byte, 8))
	writeErr := a.WriteFrame([]byte{1})

	if !errors.Is(readErr, ErrClosed) {
		t.Errorf("ReadFrame: got %v, want ErrClosed", readErr)
	}
	if !errors.Is(writeErr, ErrClosed) {
		t.Errorf("WriteFrame: got %v, want ErrClosed", writeErr)
	}
}

func TestMemPair_Closeは何度呼んでもエラーにならない(t *testing.T) {
	a, b := NewMemPair("a", "b")

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close on same end: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close on peer: %v", err)
	}
}
