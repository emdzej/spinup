package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

// GET /api/v1/applications/{appId}/logs?follow=true&tail=N
//
// Streams the function pod's stdout. Currently pod-scoped: for multi-replica
// SpinApps, we tail whichever pod client-go returns first — good enough for
// the demo, fan-out is a follow-up.
//
// workerpool runtime doesn't currently surface per-app logs (the worker
// process's stdout mixes all apps). Returns 501 with a hint.
func (s *Server) getApplicationLogs(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	if app.Runtime == store.RuntimeWorkerPool {
		http.Error(w, "logs for workerpool apps not yet surfaced — check the spinup-worker pod's stdout", http.StatusNotImplemented)
		return
	}

	follow := r.URL.Query().Get("follow") == "true"
	tail := int64(200)
	if s := r.URL.Query().Get("tail"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			tail = n
		}
	}

	// SpinKube stamps every function pod with `core.spinkube.dev/app-name=<app>`
	// (same label kube_pod_labels + the metrics query rely on).
	selector := "core.spinkube.dev/app-name=" + app.Name
	stream, err := s.builder.PodLogsByLabel(r.Context(), selector, follow, tail)
	if err != nil {
		s.logger.Warn("app logs", "err", err, "app", app.Name)
		http.Error(w, "logs unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if stream == nil {
		http.Error(w, "no running pod for this application yet", http.StatusAccepted)
		return
	}
	defer stream.Close()

	w.Header().Set("content-type", "text/plain; charset=utf-8")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("x-accel-buffering", "no")
	flusher, _ := w.(http.Flusher)
	// Force headers + an initial byte so intermediate proxies (Vite in dev,
	// nginx/istio in prod) start streaming to the client instead of buffering
	// until the first pod-log byte arrives. A bare WriteHeader+Flush isn't
	// enough — Go's http server only kicks the chunked transfer encoding
	// on the first actual body write.
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, 4096)
	for {
		n, rerr := stream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr == io.EOF {
			return
		}
		if rerr != nil {
			return
		}
	}
}
