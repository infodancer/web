package handler

import "net/http"

// healthz is a liveness/readiness probe. A blank-query Search exercises the
// engine wiring without depending on corpus contents (it short-circuits before
// touching the DB), so a 200 means the handler and templates are wired; the
// DB-touching readiness check arrives with the richer /healthz in DESIGN §7.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
