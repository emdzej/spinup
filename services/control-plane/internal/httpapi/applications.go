package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/google/uuid"

	"github.com/emdzej/spinup/services/control-plane/internal/auth"
	"github.com/emdzej/spinup/services/control-plane/internal/deploy"
	"github.com/emdzej/spinup/services/control-plane/internal/spinapp"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

// V1 is single-tenant; tenant id is a placeholder until multi-tenancy lands.
const defaultTenant = "default"

// dns1123 lower-case alphanumeric labels separated by '-', starting/ending
// alphanumeric. Max 63 chars per Kubernetes.
var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

type applicationDTO struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Language    string        `json:"language"`
	Runtime     string        `json:"runtime"`
	Description string        `json:"description,omitempty"`
	Functions   []functionDTO `json:"functions,omitempty"`
	Deployment  *deploymentVW `json:"deployment,omitempty"`
}

type functionDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Route string `json:"route"`
}

type createApplicationInput struct {
	Name        string `json:"name"`
	Language    string `json:"language"`
	Runtime     string `json:"runtime,omitempty"` // "spinkube" (default) or "workerpool"
	Description string `json:"description,omitempty"`
}

type deploymentVW struct {
	Image            string `json:"image"`
	ImageSizeBytes   *int64 `json:"imageSizeBytes,omitempty"`
	Replicas         int32  `json:"replicas"`
	ObservedReplicas int32  `json:"observedReplicas"`
	Ready            bool   `json:"ready"`
	Message          string `json:"message,omitempty"`
	Namespace        string `json:"namespace"`
	ServiceName      string `json:"serviceName"`
	InternalURL      string `json:"internalUrl"`
	PublicURL        string `json:"publicUrl,omitempty"`
}

func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	apps, err := s.store.ListApplications(r.Context(), defaultTenant)
	if err != nil {
		s.logger.Error("list applications", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]applicationDTO, 0, len(apps))
	for _, a := range apps {
		out = append(out, applicationDTO{
			ID: a.ID, Name: a.Name, Language: a.Language,
			Runtime: string(a.Runtime), Description: a.Description,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// createApplication auto-creates one Function inside the new App with the
// same name and route "/...". Multi-function apps come from POSTing to the
// nested functions endpoint after the app exists.
func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	var in createApplicationInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if !dns1123.MatchString(in.Name) {
		http.Error(w, "name must be DNS-1123 (lowercase alphanumeric + '-', 1-63 chars)", http.StatusBadRequest)
		return
	}
	switch in.Language {
	case "go", "js", "ts", "rust":
	default:
		http.Error(w, "unsupported language", http.StatusBadRequest)
		return
	}
	runtime := store.Runtime(in.Runtime)
	if runtime == "" {
		runtime = store.RuntimeSpinKube
	}
	switch runtime {
	case store.RuntimeSpinKube, store.RuntimeWorkerPool:
	default:
		http.Error(w, `runtime must be "spinkube" or "workerpool"`, http.StatusBadRequest)
		return
	}

	app := store.Application{
		ID:          uuid.NewString(),
		TenantID:    defaultTenant,
		Name:        in.Name,
		Language:    in.Language,
		Runtime:     runtime,
		Description: in.Description,
	}
	if err := s.store.CreateApplication(r.Context(), app); err != nil {
		s.logger.Error("create application", "err", err)
		http.Error(w, "internal error (possibly duplicate name): "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Auto-create the first function.
	fn := store.Function{
		ID:            uuid.NewString(),
		ApplicationID: app.ID,
		Name:          app.Name,
		Route:         "/...",
	}
	if err := s.store.CreateFunction(r.Context(), fn); err != nil {
		// Best-effort cleanup.
		_ = s.store.DeleteApplication(r.Context(), defaultTenant, app.ID)
		s.logger.Error("create default function", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.metrics.FunctionsCreated.Add(r.Context(), 1)

	writeJSON(w, http.StatusCreated, applicationDTO{
		ID: app.ID, Name: app.Name, Language: app.Language,
		Runtime: string(app.Runtime), Description: app.Description,
		Functions: []functionDTO{{ID: fn.ID, Name: fn.Name, Route: fn.Route}},
	})
}

func (s *Server) getApplication(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("appId")
	app, ok := s.loadApplication(w, r, id)
	if !ok {
		return
	}
	fns, err := s.store.ListFunctions(r.Context(), app.ID)
	if err != nil {
		s.logger.Error("list functions for app", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := applicationDTO{
		ID: app.ID, Name: app.Name, Language: app.Language,
		Runtime: string(app.Runtime), Description: app.Description,
	}
	for _, f := range fns {
		out.Functions = append(out.Functions, functionDTO{ID: f.ID, Name: f.Name, Route: f.Route})
	}

	if app.Runtime == store.RuntimeWorkerPool {
		// Deployment status = latest successful build's image, if any.
		builds, err := s.store.ListBuilds(r.Context(), app.ID, 20)
		if err == nil {
			for _, b := range builds {
				if b.Status == store.BuildSucceeded {
					out.Deployment = &deploymentVW{
						Image: b.ImageRef, Replicas: 0, ObservedReplicas: 0,
						Ready: true, Message: "served by worker pool on demand",
						Namespace: "", ServiceName: app.Name,
						InternalURL: workerInvokeURL(s.worker.publicURLForUI(), app.Name),
						PublicURL:   "",
					}
					break
				}
			}
		}
	} else if st, err := s.spin.Get(r.Context(), app.Name); err != nil {
		s.logger.Warn("get spinapp", "err", err, "name", app.Name)
	} else if st != nil {
		out.Deployment = s.buildDeploymentVW(app, st)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteApplication(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("appId")
	app, ok := s.loadApplication(w, r, id)
	if !ok {
		return
	}
	if app.Runtime != store.RuntimeWorkerPool {
		if err := s.deployer.Undeploy(r.Context(), app.Name); err != nil {
			s.logger.Error("undeploy application", "err", err, "name", app.Name)
			http.Error(w, "undeploy: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	if err := s.store.DeleteApplication(r.Context(), defaultTenant, app.ID); err != nil {
		s.logger.Error("delete application", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.metrics.FunctionsDeleted.Add(r.Context(), 1)
	w.WriteHeader(http.StatusNoContent)
}

// deployApplication applies a SpinApp CR using a caller-provided OCI image.
// Used for "deploy from image" flows; auto-deploy after build lives in the builder.
func (s *Server) deployApplication(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("appId")
	app, ok := s.loadApplication(w, r, id)
	if !ok {
		return
	}
	var in struct {
		Image    string `json:"image"`
		Replicas int32  `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if in.Image == "" {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}
	if in.Replicas < 1 {
		in.Replicas = 1
	}
	st, err := s.deployer.Deploy(r.Context(), deploy.Request{
		App:      app,
		Image:    in.Image,
		Replicas: in.Replicas,
	})
	if err != nil {
		s.logger.Error("deploy application", "err", err, "name", app.Name)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.metrics.DeploysApplied.Add(r.Context(), 1)
	writeJSON(w, http.StatusOK, s.buildDeploymentVW(app, st))
}

func (s *Server) buildDeploymentVW(app store.Application, st *spinapp.Status) *deploymentVW {
	d := &deploymentVW{
		Image:            st.Image,
		Replicas:         st.Replicas,
		ObservedReplicas: st.ObservedReplicas,
		Ready:            st.Ready,
		Message:          st.Message,
		Namespace:        s.functions.Namespace,
		ServiceName:      app.Name,
		InternalURL:      "http://" + app.Name + "." + s.functions.Namespace + ".svc.cluster.local",
	}
	// Prefer the per-app subdomain (matches what the CP now emits via
	// VirtualService). Fall back to the legacy /fn/{name} form when only
	// PublicBaseURL is set (headless / bearer-only deployments).
	switch {
	case s.functions.PublicDomain != "":
		d.PublicURL = "https://" + app.Name + "." + s.functions.PublicDomain
	case s.functions.PublicBaseURL != "":
		d.PublicURL = s.functions.PublicBaseURL + "/fn/" + app.Name
	}
	// Copy the image size from the build that produced the currently-running
	// image, if we can find one. Cheap lookup — the builds table has an index
	// on (application_id, created_at DESC) and we filter by imageRef.
	if st.Image != "" {
		if size, ok := s.lookupImageSize(app.ID, st.Image); ok {
			d.ImageSizeBytes = &size
		}
	}
	return d
}

// lookupImageSize scans recent builds for one whose imageRef matches the
// currently deployed image, and returns its stored size. Returns (0, false)
// when no matching build has a stored size (e.g. older builds from before
// the size-reporting change landed).
func (s *Server) lookupImageSize(applicationID, imageRef string) (int64, bool) {
	builds, err := s.store.ListBuilds(context.Background(), applicationID, 20)
	if err != nil {
		return 0, false
	}
	for _, b := range builds {
		if b.ImageRef == imageRef && b.ImageSizeBytes != nil {
			return *b.ImageSizeBytes, true
		}
	}
	return 0, false
}

func (s *Server) loadApplication(w http.ResponseWriter, r *http.Request, id string) (store.Application, bool) {
	app, err := s.store.GetApplication(r.Context(), defaultTenant, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "application not found", http.StatusNotFound)
			return store.Application{}, false
		}
		s.logger.Error("load application", "err", err, "id", id)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Application{}, false
	}
	return app, true
}

func (s *Server) loadFunction(w http.ResponseWriter, r *http.Request, appID, fnID string) (store.Application, store.Function, bool) {
	app, ok := s.loadApplication(w, r, appID)
	if !ok {
		return store.Application{}, store.Function{}, false
	}
	fn, err := s.store.GetFunction(r.Context(), appID, fnID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "function not found", http.StatusNotFound)
			return store.Application{}, store.Function{}, false
		}
		s.logger.Error("load function", "err", err, "fn", fnID, "app", appID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return store.Application{}, store.Function{}, false
	}
	return app, fn, true
}

func authed(r *http.Request) bool {
	_, ok := auth.ClaimsFrom(r.Context())
	return ok
}
