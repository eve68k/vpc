// Package ctl は Proxmox の hookscript などから VNI の増減を受け付ける、unix socket 上の HTTP API。
//
//	PUT    /vnis/{name}  body: {"vpc_id": 1, "mac": "02:00:00:00:00:01"}
//	DELETE /vnis/{name}
//
// agent.Source を実装する。
package ctl

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/eve68k/vpc/internal/agent"
	"github.com/eve68k/vpc/internal/mapping"
)

// Registry は PUT で渡された VNI の情報を、agent が VNIForPort で引ける場所に登録する。
// 本物のマッピングサービスが VNI を保持するようになれば不要になる、モック用の口。
type Registry interface {
	Register(v mapping.VNI)
	Unregister(name string)
}

type Server struct {
	Path     string
	Registry Registry

	mu  sync.Mutex
	srv *http.Server
}

type putRequest struct {
	VPCID uint32 `json:"vpc_id"`
	MAC   string `json:"mac"`
}

// Run は Path の unix socket で待ち受ける。Close されると nil を返す。
func (s *Server) Run(sink agent.Sink) error {
	// 前回の異常終了で残ったソケットファイルがあると Listen に失敗する。
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	l, err := net.Listen("unix", s.Path)
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: s.Handler(sink)}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()

	if err := srv.Serve(l); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

// Handler は sink に対する HTTP ハンドラを返す。
func (s *Server) Handler(sink agent.Sink) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("PUT /vnis/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var req putRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mac, err := net.ParseMAC(req.MAC)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		s.Registry.Register(mapping.VNI{Name: name, VPCID: mapping.VPCID(req.VPCID), MAC: mac})
		if err := sink.Attach(name); err != nil {
			// 既に動いている Port の登録まで消さない。
			if !errors.Is(err, agent.ErrAlreadyAttached) {
				s.Registry.Unregister(name)
			}
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /vnis/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		err := sink.Detach(name)
		s.Registry.Unregister(name)
		if err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, agent.ErrAlreadyAttached):
		code = http.StatusConflict
	case errors.Is(err, agent.ErrNotAttached):
		code = http.StatusNotFound
	case errors.Is(err, agent.ErrUnknownVNI):
		code = http.StatusBadRequest
	}
	http.Error(w, err.Error(), code)
}
