package httpapi

import (
	"net/http"

	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

// workerAppEntry is what the spinup-worker binary consumes.
type workerAppEntry struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Language    string             `json:"language"`
	ImageRef    string             `json:"imageRef"`
	Description string             `json:"description,omitempty"`
	Functions   []workerFunctionKV `json:"functions,omitempty"`
}

type workerFunctionKV struct {
	Name  string `json:"name"`
	Route string `json:"route"`
}

type workerConfigDTO struct {
	Apps []workerAppEntry `json:"apps"`
}

// GET /api/v1/worker-config
//
// Consumed by the spinup-worker binary via periodic polling. Returns every
// workerpool-runtime Application that has a successful build, along with its
// latest OCI image and its functions.
//
// This endpoint sits *inside* /api/* so it's OIDC-gated in production; the
// worker uses a bearer token from its ServiceAccount to authenticate.
func (s *Server) getWorkerConfig(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	apps, err := s.store.ListApplications(r.Context(), defaultTenant)
	if err != nil {
		s.logger.Error("list apps for worker config", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Initialize as empty (not nil) so JSON emits `[]` — makes stricter
	// deserializers on the client side happy (see: Rust worker's serde).
	out := workerConfigDTO{Apps: []workerAppEntry{}}
	for _, a := range apps {
		if a.Runtime != store.RuntimeWorkerPool {
			continue
		}
		image := latestSuccessfulImage(r, s, a.ID)
		if image == "" {
			continue // no successful build yet — worker skips it
		}
		fns, _ := s.store.ListFunctions(r.Context(), a.ID)
		entry := workerAppEntry{
			ID: a.ID, Name: a.Name, Language: a.Language,
			ImageRef: image, Description: a.Description,
		}
		for _, f := range fns {
			entry.Functions = append(entry.Functions, workerFunctionKV{Name: f.Name, Route: f.Route})
		}
		out.Apps = append(out.Apps, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

func latestSuccessfulImage(r *http.Request, s *Server, appID string) string {
	builds, err := s.store.ListBuilds(r.Context(), appID, 30)
	if err != nil {
		return ""
	}
	for _, b := range builds {
		if b.Status == store.BuildSucceeded {
			return b.ImageRef
		}
	}
	return ""
}
