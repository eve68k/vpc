package ctl

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eve68k/vpc/internal/agent"
	"github.com/eve68k/vpc/internal/mapping"
)

type fakeSink struct {
	attached map[string]bool
	err      error
}

func (f *fakeSink) Attach(name string) error {
	if f.err != nil {
		return f.err
	}
	f.attached[name] = true
	return nil
}

func (f *fakeSink) Detach(name string) error {
	if !f.attached[name] {
		return agent.ErrNotAttached
	}
	delete(f.attached, name)
	return nil
}

type fakeRegistry map[string]mapping.VNI

func (r fakeRegistry) Register(v mapping.VNI) { r[v.Name] = v }
func (r fakeRegistry) Unregister(name string) { delete(r, name) }

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

const putBody = `{"vpc_id": 7, "mac": "02:00:00:00:00:01"}`

func TestPUT_VNIを登録してAttachする(t *testing.T) {
	sink, reg := &fakeSink{attached: map[string]bool{}}, fakeRegistry{}
	h := (&Server{Registry: reg}).Handler(sink)

	rec := do(h, "PUT", "/vnis/tap100i0", putBody)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if !sink.attached["tap100i0"] {
		t.Error("Attach されていない")
	}
	if v := reg["tap100i0"]; v.VPCID != 7 || v.MAC.String() != "02:00:00:00:00:01" {
		t.Errorf("registry = %+v", v)
	}
}

func TestPUT_不正なリクエストはAttachしない(t *testing.T) {
	for name, body := range map[string]string{
		"JSONでない": "x",
		"MACが不正":  `{"vpc_id": 1, "mac": "zz"}`,
	} {
		sink, reg := &fakeSink{attached: map[string]bool{}}, fakeRegistry{}
		rec := do((&Server{Registry: reg}).Handler(sink), "PUT", "/vnis/tap0", body)
		if rec.Code != http.StatusBadRequest || len(sink.attached) != 0 || len(reg) != 0 {
			t.Errorf("%s: status=%d attached=%v registry=%v", name, rec.Code, sink.attached, reg)
		}
	}
}

func TestPUT_Attachに失敗したら登録を戻す(t *testing.T) {
	sink, reg := &fakeSink{err: net.ErrClosed}, fakeRegistry{}
	rec := do((&Server{Registry: reg}).Handler(sink), "PUT", "/vnis/tap0", putBody)

	if rec.Code != http.StatusInternalServerError || len(reg) != 0 {
		t.Errorf("status=%d registry=%v", rec.Code, reg)
	}
}

func TestPUT_Attach済みなら409で既存の登録を消さない(t *testing.T) {
	sink, reg := &fakeSink{err: agent.ErrAlreadyAttached}, fakeRegistry{}
	rec := do((&Server{Registry: reg}).Handler(sink), "PUT", "/vnis/tap0", putBody)

	if rec.Code != http.StatusConflict || len(reg) != 1 {
		t.Errorf("status=%d registry=%v", rec.Code, reg)
	}
}

func TestDELETE_DetachしてVNIを取り除く(t *testing.T) {
	sink, reg := &fakeSink{attached: map[string]bool{"tap0": true}}, fakeRegistry{"tap0": {Name: "tap0"}}
	h := (&Server{Registry: reg}).Handler(sink)

	if rec := do(h, "DELETE", "/vnis/tap0", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(sink.attached) != 0 || len(reg) != 0 {
		t.Errorf("attached=%v registry=%v", sink.attached, reg)
	}
	if rec := do(h, "DELETE", "/vnis/tap0", ""); rec.Code != http.StatusNotFound {
		t.Errorf("2回目 status = %d, want 404", rec.Code)
	}
}

func TestRun_unix_socket越しに操作できCloseで止まる(t *testing.T) {
	// t.TempDir() はテスト名を含み、unix socket のパス長の上限(macOSは104)を超えうる。
	dir, err := os.MkdirTemp("", "ctl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "a.sock")
	sink := &fakeSink{attached: map[string]bool{}}
	s := &Server{Path: path, Registry: fakeRegistry{}}

	done := make(chan error, 1)
	go func() { done <- s.Run(sink) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
	var resp *http.Response
	for i := 0; i < 50; i++ { // Listen の開始を待つ。
		req, _ := http.NewRequest("PUT", "http://agent/vnis/tap0", strings.NewReader(putBody))
		if resp, err = client.Do(req); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent || !sink.attached["tap0"] {
		t.Errorf("status=%d attached=%v", resp.StatusCode, sink.attached)
	}

	s.Close()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
}
