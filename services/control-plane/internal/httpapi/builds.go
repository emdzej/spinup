package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emdzej/spinup/services/control-plane/internal/builder"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

type sourceDTO struct {
	Files     map[string]string `json:"files"`
	UpdatedAt time.Time         `json:"updatedAt,omitempty"`
}

type buildDTO struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	ImageRef       string     `json:"imageRef"`
	ImageSizeBytes *int64     `json:"imageSizeBytes,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

// GET /api/v1/applications/{appId}/functions/{fnId}/source
func (s *Server) getSource(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	_, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	src, err := s.store.GetSource(r.Context(), fn.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, sourceDTO{Files: map[string]string{}})
			return
		}
		s.logger.Error("get source", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sourceDTO{Files: src.Files, UpdatedAt: src.UpdatedAt})
}

// PUT /api/v1/applications/{appId}/functions/{fnId}/source
func (s *Server) putSource(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	_, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	var in sourceDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(in.Files) == 0 {
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}
	for name := range in.Files {
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			http.Error(w, "invalid file path: "+name, http.StatusBadRequest)
			return
		}
	}
	if err := s.store.PutSource(r.Context(), store.Source{FunctionID: fn.ID, Files: in.Files}); err != nil {
		s.logger.Error("put source", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sourceDTO{Files: in.Files})
}

// GET /api/v1/applications/{appId}/builds
func (s *Server) listBuilds(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	builds, err := s.store.ListBuilds(r.Context(), app.ID, 50)
	if err != nil {
		s.logger.Error("list builds", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]buildDTO, 0, len(builds))
	for _, b := range builds {
		out = append(out, toBuildDTO(b))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/v1/applications/{appId}/builds — packs all the app's functions
// into one OCI image via a synthesized multi-component spin.toml.
func (s *Server) startBuild(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	fns, err := s.store.ListFunctions(r.Context(), app.ID)
	if err != nil {
		s.logger.Error("list functions", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(fns) == 0 {
		http.Error(w, "application has no functions", http.StatusBadRequest)
		return
	}

	inputs := make([]builder.FunctionBuildInput, 0, len(fns))
	for _, fn := range fns {
		src, err := s.store.GetSource(r.Context(), fn.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "function "+fn.Name+" has no source uploaded yet", http.StatusBadRequest)
				return
			}
			s.logger.Error("get source", "err", err, "fn", fn.Name)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		inputs = append(inputs, builder.FunctionBuildInput{Function: fn, Source: src})
	}

	build, err := s.builder.Start(r.Context(), app, inputs)
	if err != nil {
		s.logger.Error("start build", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusAccepted, toBuildDTO(build))
}

// GET /api/v1/applications/{appId}/builds/{buildId}
func (s *Server) getBuild(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	b, err := s.store.GetBuild(r.Context(), app.ID, r.PathValue("buildId"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get build", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, toBuildDTO(b))
}

// GET /api/v1/applications/{appId}/builds/{buildId}/logs
func (s *Server) getBuildLogs(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	buildID := r.PathValue("buildId")
	if _, err := s.store.GetBuild(r.Context(), app.ID, buildID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.logger.Error("get build for logs", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	follow := r.URL.Query().Get("follow") == "true"
	stream, err := s.builder.Logs(r.Context(), buildID, follow)
	if err != nil {
		s.logger.Warn("build logs", "err", err, "build_id", buildID)
		http.Error(w, "logs unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if stream == nil {
		http.Error(w, "no pod for build yet", http.StatusAccepted)
		return
	}
	defer stream.Close()

	w.Header().Set("content-type", "text/plain; charset=utf-8")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("x-accel-buffering", "no")

	flusher, _ := w.(http.Flusher)
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

func toBuildDTO(b store.Build) buildDTO {
	return buildDTO{
		ID:             b.ID,
		Status:         string(b.Status),
		ImageRef:       b.ImageRef,
		ImageSizeBytes: b.ImageSizeBytes,
		Error:          b.Error,
		CreatedAt:      b.CreatedAt,
		FinishedAt:     b.FinishedAt,
	}
}
