package server

import (
	"context"
	"net/http"

	"github.com/auxitalk/plugin-dashboard/internal/templates"
)

type Server struct {
	httpServer *http.Server
}

func NewServer(port string) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", render(templates.Index()))
	mux.HandleFunc("/plugins", render(templates.Plugins()))
	mux.HandleFunc("/sessions", render(templates.Sessions()))
	mux.HandleFunc("/events", render(templates.Events()))
	mux.HandleFunc("/actions", render(templates.Actions()))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"service":"auxitalk-dashboard"}`))
	})

	mux.HandleFunc("/actions/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="text-emerald-400">Action approved.</div>`))
	})

	mux.HandleFunc("/actions/reject", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="text-red-400">Action rejected.</div>`))
	})

	return &Server{
		httpServer: &http.Server{
			Addr:    ":" + port,
			Handler: mux,
		},
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
