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
	Replicas    int32         `json:"replicas"`
	Variables   []variableDTO `json:"variables"`
	Resources   resourcesDTO  `json:"resources"`
	Functions   []functionDTO `json:"functions,omitempty"`
	Deployment  *deploymentVW `json:"deployment,omitempty"`
}

type variableDTO struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type resourcesDTO struct {
	CPURequest    string `json:"cpuRequest,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
}

type updateApplicationInput struct {
	Description *string       `json:"description,omitempty"`
	Replicas    *int32        `json:"replicas,omitempty"`
	Variables   []variableDTO `json:"variables,omitempty"`
	Resources   *resourcesDTO `json:"resources,omitempty"`
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
	UpdatedReplicas  int32  `json:"updatedReplicas"`
	Ready            bool   `json:"ready"`
	// Progressing is true during a rollout: old pod is still Ready but the
	// new pod is not yet Available. The UI shows this distinctly from Ready
	// so users don't invoke and get stale results.
	Progressing bool   `json:"progressing"`
	Message     string `json:"message,omitempty"`
	Namespace   string `json:"namespace"`
	ServiceName string `json:"serviceName"`
	InternalURL string `json:"internalUrl"`
	PublicURL   string `json:"publicUrl,omitempty"`
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
		out = append(out, toApplicationDTO(a, nil, nil))
	}
	writeJSON(w, http.StatusOK, out)
}

// toApplicationDTO renders the persistent app row into the API shape used by
// the list / get / update responses. functions and deployment are optional —
// callers pass nil when the endpoint doesn't need them (list view).
func toApplicationDTO(a store.Application, functions []functionDTO, deployment *deploymentVW) applicationDTO {
	vars := make([]variableDTO, 0, len(a.Variables))
	for _, v := range a.Variables {
		vars = append(vars, variableDTO{Name: v.Name, Value: v.Value})
	}
	return applicationDTO{
		ID:          a.ID,
		Name:        a.Name,
		Language:    a.Language,
		Runtime:     string(a.Runtime),
		Description: a.Description,
		Replicas:    a.Replicas,
		Variables:   vars,
		Resources: resourcesDTO{
			CPURequest:    a.Resources.CPURequest,
			CPULimit:      a.Resources.CPULimit,
			MemoryRequest: a.Resources.MemoryRequest,
			MemoryLimit:   a.Resources.MemoryLimit,
		},
		Functions:  functions,
		Deployment: deployment,
	}
}

// createApplication auto-creates one starter Function named "default" so
// the user has something to edit right away. The name is intentionally NOT
// the application's name — those are different things (an app can hold many
// functions, and renaming the app shouldn't drag a mismatched function name
// along). Multi-function apps come from POSTing to the nested functions
// endpoint after the app exists.
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
		Replicas:    1,
	}
	if err := s.store.CreateApplication(r.Context(), app); err != nil {
		s.logger.Error("create application", "err", err)
		http.Error(w, "internal error (possibly duplicate name): "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Auto-create the first function. Fixed name "default"; route "/..."
	// catches everything under the app hostname, so a single-function app
	// works with no path segment at all. When users add a second function
	// they typically narrow both routes.
	fn := store.Function{
		ID:            uuid.NewString(),
		ApplicationID: app.ID,
		Name:          "default",
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
	fnDTOs := make([]functionDTO, 0, len(fns))
	for _, f := range fns {
		fnDTOs = append(fnDTOs, functionDTO{ID: f.ID, Name: f.Name, Route: f.Route})
	}
	var dep *deploymentVW
	if app.Runtime == store.RuntimeWorkerPool {
		// Deployment status = latest successful build's image, if any.
		builds, err := s.store.ListBuilds(r.Context(), app.ID, 20)
		if err == nil {
			for _, b := range builds {
				if b.Status == store.BuildSucceeded {
					dep = &deploymentVW{
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
		dep = s.buildDeploymentVW(app, st)
	}
	writeJSON(w, http.StatusOK, toApplicationDTO(app, fnDTOs, dep))
}

// updateApplication is the config-side PATCH: description, replicas, variables,
// resources. All fields are optional; a nil pointer / omitted key leaves the
// existing value alone. When the app has an active deployment we re-Apply
// the SpinApp so replicas/vars/resources take effect immediately; if the
// user changes only description we skip the reconcile.
func (s *Server) updateApplication(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, ok := s.loadApplication(w, r, r.PathValue("appId"))
	if !ok {
		return
	}
	var in updateApplicationInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	desc := app.Description
	if in.Description != nil {
		desc = *in.Description
	}
	replicas := app.Replicas
	if in.Replicas != nil {
		replicas = *in.Replicas
	}
	if replicas < 0 {
		http.Error(w, "replicas must be >= 0", http.StatusBadRequest)
		return
	}
	// Variables: if the field is present in the request body, replace the
	// whole list — variables are keyed by name so partial updates would be
	// ambiguous. A caller that wants to clear all vars sends `"variables": []`.
	vars := app.Variables
	if in.Variables != nil {
		vars = vars[:0]
		for _, v := range in.Variables {
			if !isVariableName(v.Name) {
				http.Error(w, "variable name must start with a letter or '_' and contain only [A-Za-z0-9_]", http.StatusBadRequest)
				return
			}
			vars = append(vars, store.Variable{Name: v.Name, Value: v.Value})
		}
	}
	res := app.Resources
	if in.Resources != nil {
		res = store.Resources{
			CPURequest:    in.Resources.CPURequest,
			CPULimit:      in.Resources.CPULimit,
			MemoryRequest: in.Resources.MemoryRequest,
			MemoryLimit:   in.Resources.MemoryLimit,
		}
	}

	if err := s.store.UpdateApplicationConfig(r.Context(), defaultTenant, app.ID, desc, replicas, vars, res); err != nil {
		s.logger.Error("update application config", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	app.Description, app.Replicas, app.Variables, app.Resources = desc, replicas, vars, res

	// Reconcile with the cluster only if there's already a deployed SpinApp
	// AND the change actually affects it (description-only PATCHes stay a
	// no-op on the cluster).
	if app.Runtime != store.RuntimeWorkerPool && (in.Replicas != nil || in.Variables != nil || in.Resources != nil) {
		if st, err := s.spin.Get(r.Context(), app.Name); err == nil && st != nil && st.Image != "" {
			if _, err := s.deployer.Deploy(r.Context(), deploy.Request{App: app, Image: st.Image}); err != nil {
				s.logger.Warn("re-deploy after config update", "err", err, "name", app.Name)
			}
		}
	}

	fns, _ := s.store.ListFunctions(r.Context(), app.ID)
	fnDTOs := make([]functionDTO, 0, len(fns))
	for _, f := range fns {
		fnDTOs = append(fnDTOs, functionDTO{ID: f.ID, Name: f.Name, Route: f.Route})
	}
	writeJSON(w, http.StatusOK, toApplicationDTO(app, fnDTOs, nil))
}

// isVariableName is a permissive check for `variables[].name`: letters, digits,
// underscore, must start with a letter or underscore. Spin uses these as
// identifiers inside components, matches what the SDK accepts.
var _envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isVariableName(s string) bool { return _envVarName.MatchString(s) }

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
		UpdatedReplicas:  st.UpdatedReplicas,
		Ready:            st.Ready,
		Progressing:      st.Progressing,
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
