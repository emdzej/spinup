package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

type createFunctionInput struct {
	Name  string `json:"name"`
	Route string `json:"route,omitempty"`
}

// POST /api/v1/applications/{appId}/functions — add a new function to an existing App.
func (s *Server) createFunction(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	var in createFunctionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if !dns1123.MatchString(in.Name) {
		http.Error(w, "name must be DNS-1123", http.StatusBadRequest)
		return
	}
	if in.Route == "" {
		in.Route = "/" + in.Name + "/..."
	}
	fn := store.Function{
		ID:            uuid.NewString(),
		ApplicationID: app.ID,
		Name:          in.Name,
		Route:         in.Route,
	}
	if err := s.store.CreateFunction(r.Context(), fn); err != nil {
		s.logger.Error("create function", "err", err)
		http.Error(w, "internal error (possibly duplicate name/route): "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.metrics.FunctionsCreated.Add(r.Context(), 1)
	writeJSON(w, http.StatusCreated, functionDTO{ID: fn.ID, Name: fn.Name, Route: fn.Route})
}

// GET /api/v1/applications/{appId}/functions
func (s *Server) listFunctions(w http.ResponseWriter, r *http.Request) {
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
	out := make([]functionDTO, 0, len(fns))
	for _, f := range fns {
		out = append(out, functionDTO{ID: f.ID, Name: f.Name, Route: f.Route})
	}
	writeJSON(w, http.StatusOK, out)
}

// PUT /api/v1/applications/{appId}/functions/{fnId} — currently supports
// updating the trigger route. Route changes only take effect after the next
// build (the route is baked into spin.toml in the OCI image).
func (s *Server) updateFunction(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	var in struct {
		Route string `json:"route"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if in.Route == "" || in.Route[0] != '/' {
		http.Error(w, `route must start with "/"`, http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateFunctionRoute(r.Context(), app.ID, fn.ID, in.Route); err != nil {
		s.logger.Error("update function route", "err", err)
		http.Error(w, "internal error (possibly duplicate route)", http.StatusInternalServerError)
		return
	}
	fn.Route = in.Route
	writeJSON(w, http.StatusOK, functionDTO{ID: fn.ID, Name: fn.Name, Route: fn.Route})
}

// GET /api/v1/applications/{appId}/functions/{fnId}
func (s *Server) getFunction(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	_, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, functionDTO{ID: fn.ID, Name: fn.Name, Route: fn.Route})
}

// DELETE /api/v1/applications/{appId}/functions/{fnId} — refuses to remove the
// last function of an app; delete the whole app instead.
func (s *Server) deleteFunction(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	all, err := s.store.ListFunctions(r.Context(), app.ID)
	if err != nil {
		s.logger.Error("list functions before delete", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(all) <= 1 {
		http.Error(w, "cannot delete last function; delete the application instead", http.StatusConflict)
		return
	}
	if err := s.store.DeleteFunction(r.Context(), app.ID, fn.ID); err != nil {
		s.logger.Error("delete function", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.metrics.FunctionsDeleted.Add(r.Context(), 1)
	w.WriteHeader(http.StatusNoContent)
}
