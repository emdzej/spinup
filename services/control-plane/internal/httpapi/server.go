package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/emdzej/spinup/services/control-plane/internal/auth"
	"github.com/emdzej/spinup/services/control-plane/internal/builder"
	"github.com/emdzej/spinup/services/control-plane/internal/config"
	"github.com/emdzej/spinup/services/control-plane/internal/deploy"
	"github.com/emdzej/spinup/services/control-plane/internal/promql"
	"github.com/emdzej/spinup/services/control-plane/internal/proxy"
	"github.com/emdzej/spinup/services/control-plane/internal/spinapp"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
	"github.com/emdzej/spinup/services/control-plane/internal/telemetry"
)

type Server struct {
	logger    *slog.Logger
	version   string
	store     store.Store
	verifier  *auth.Verifier
	spin      *spinapp.Client
	deployer  *deploy.Deployer
	builder   *builder.Runner
	metrics   *telemetry.Metrics
	functions config.FunctionsConfig
	prom      *promql.Client
	proxy     *proxy.Client
	worker    workerRuntime
}

func New(logger *slog.Logger, version string, st store.Store, v *auth.Verifier, oa *auth.OAuth, spin *spinapp.Client, dep *deploy.Deployer, b *builder.Runner, m *telemetry.Metrics, metricsHandler http.Handler, fns config.FunctionsConfig, prom *promql.Client, prx *proxy.Client, wcfg config.WorkerConfig, uiHandler http.Handler) http.Handler {
	s := &Server{
		logger: logger, version: version, store: st, verifier: v, spin: spin, deployer: dep, builder: b, metrics: m,
		functions: fns, prom: prom, proxy: prx,
		worker: workerRuntime{invokeURL: wcfg.URL, uiURL: wcfg.UIURL},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/version", s.versionInfo)
	mux.Handle("GET /metrics", metricsHandler)

	// OIDC browser flow: /auth/login, /auth/callback, /auth/logout, /auth/me.
	// In dev-skip mode (oa == nil) we still register /auth/me returning a
	// synthetic user so the UI's bootstrap keeps working.
	auth.Register(mux, v, oa)

	api := http.NewServeMux()

	// Applications
	api.HandleFunc("GET /api/v1/applications", s.listApplications)
	api.HandleFunc("POST /api/v1/applications", s.createApplication)
	api.HandleFunc("GET /api/v1/applications/{appId}", s.getApplication)
	api.HandleFunc("DELETE /api/v1/applications/{appId}", s.deleteApplication)
	api.HandleFunc("POST /api/v1/applications/{appId}/deploy", s.deployApplication)

	// Functions (nested under app)
	api.HandleFunc("GET /api/v1/applications/{appId}/functions", s.listFunctions)
	api.HandleFunc("POST /api/v1/applications/{appId}/functions", s.createFunction)
	api.HandleFunc("GET /api/v1/applications/{appId}/functions/{fnId}", s.getFunction)
	api.HandleFunc("PUT /api/v1/applications/{appId}/functions/{fnId}", s.updateFunction)
	api.HandleFunc("DELETE /api/v1/applications/{appId}/functions/{fnId}", s.deleteFunction)

	// Source (per function)
	api.HandleFunc("GET /api/v1/applications/{appId}/functions/{fnId}/source", s.getSource)
	api.HandleFunc("PUT /api/v1/applications/{appId}/functions/{fnId}/source", s.putSource)
	api.HandleFunc("GET /api/v1/applications/{appId}/functions/{fnId}/source.tar.gz", s.exportSource)
	api.HandleFunc("POST /api/v1/applications/{appId}/functions/{fnId}/source.tar.gz", s.importSource)

	// Invoke (per function)
	api.HandleFunc("POST /api/v1/applications/{appId}/functions/{fnId}/invoke", s.invokeFunction)

	// Builds (per application)
	api.HandleFunc("GET /api/v1/applications/{appId}/builds", s.listBuilds)
	api.HandleFunc("POST /api/v1/applications/{appId}/builds", s.startBuild)
	api.HandleFunc("GET /api/v1/applications/{appId}/builds/{buildId}", s.getBuild)
	api.HandleFunc("GET /api/v1/applications/{appId}/builds/{buildId}/logs", s.getBuildLogs)

	// Metrics + logs
	api.HandleFunc("GET /api/v1/applications/{appId}/metrics", s.getApplicationMetrics)
	api.HandleFunc("GET /api/v1/applications/{appId}/functions/{fnId}/metrics", s.getFunctionMetrics)
	api.HandleFunc("GET /api/v1/applications/{appId}/logs", s.getApplicationLogs)
	api.HandleFunc("GET /api/v1/overview/metrics", s.getOverviewMetrics)

	// Worker (consumed by spinup-worker binary)
	api.HandleFunc("GET /api/v1/worker-config", s.getWorkerConfig)

	mux.Handle("/api/", m.HTTPMiddleware(v.Middleware(api)))

	// UI (SvelteKit static build) mounted at root. Registered last so /api/,
	// /healthz, /metrics take priority. When uiHandler is nil the CP serves
	// no HTML at / (useful for headless deployments).
	if uiHandler != nil {
		mux.Handle("/", uiHandler)
	}
	return mux
}

// versionInfo returns the CP version and source repo. Unauthenticated so
// the UI can render "SpinUP · v0.x" in the header before login. Cheap
// enough to serve from any client.
func (s *Server) versionInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":   s.version,
		"repoUrl":   "https://github.com/emdzej/spinup",
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "err": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
