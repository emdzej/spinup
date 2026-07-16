package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTP      HTTPConfig
	OIDC      OIDCConfig
	Authz     AuthzConfig
	DB        DBConfig
	Functions FunctionsConfig
	K8s       K8sConfig
	Builder   BuilderConfig
	Metrics   MetricsConfig
	Worker    WorkerConfig
	UI        UIConfig
}

type AuthzConfig struct {
	// RequiredRoles are matched (any-of) against the ID token's `roles` claim.
	// Empty means "authenticated is sufficient" — no role check. Env var:
	// SPINUP_AUTHZ_REQUIRED_ROLES (comma-separated).
	RequiredRoles []string
}

type UIConfig struct {
	// StaticDir, when set, points at a filesystem directory whose contents are
	// served at "/" as the UI. Empty means "use the embedded SvelteKit build"
	// (populated by the Dockerfile at internal/webui/dist/). Set this in local
	// dev to `apps/ui/build` to iterate without rebuilding the CP binary.
	StaticDir string
}

type MetricsConfig struct {
	// PrometheusURL is the base URL of a Prometheus/VictoriaMetrics HTTP API.
	// Empty disables the /api/v1/.../metrics endpoints.
	PrometheusURL string
}

type BuilderConfig struct {
	// GoImage is the container image used for Go function builds.
	GoImage string
	// JSImage is the container image used for JavaScript function builds.
	JSImage string
	// TSImage is the container image used for TypeScript function builds.
	TSImage string
	// RustImage is the container image used for Rust function builds.
	RustImage string
	// RegistryURL is the OCI registry prefix; the builder pushes to
	// {RegistryURL}/{FunctionName}:{BuildID}. e.g. "ttl.sh/spinup" or "zot:5000/spinup".
	RegistryURL string
	// AuthSecret is the name of a kubernetes.io/dockerconfigjson Secret in the
	// functions namespace. When set, it's mounted into build Jobs so `spin
	// registry push` can authenticate. Empty = anonymous registry.
	AuthSecret string
}

type FunctionsConfig struct {
	// Namespace where SpinApp resources are created.
	Namespace string
	// PublicBaseURL is the externally-reachable base URL for the /fn/{name}
	// routing convention (e.g. "https://spinup.example.com"). If unset, the UI
	// falls back to showing only the cluster-internal DNS + a port-forward hint.
	PublicBaseURL string
	// PublicDomain is the parent domain under which each function gets a
	// subdomain via a per-function Istio VirtualService (e.g. "spinup.example.com"
	// produces "hello.spinup.example.com"). Empty disables VirtualService
	// emission — functions are still reachable via the CP proxy on PublicBaseURL.
	PublicDomain string
	// PublicGateway is the "<namespace>/<name>" of the Istio Gateway the
	// per-function VirtualServices bind to (must serve the wildcard host).
	// Ignored when PublicDomain is empty.
	PublicGateway string
}

type WorkerConfig struct {
	// URL is the internal (in-cluster) base URL of the spinup-worker service.
	// e.g. "http://spinup-worker.spinup.svc.cluster.local:8000". Empty disables
	// workerpool-runtime apps.
	URL string
	// UIURL is optionally different if the worker is reachable via a distinct
	// public URL (e.g. through the same Istio gateway). Defaults to URL.
	UIURL string
}

type K8sConfig struct {
	// Kubeconfig path for local dev. Ignored when in-cluster config resolves.
	Kubeconfig string
}

type HTTPConfig struct {
	Addr string
}

type OIDCConfig struct {
	IssuerURL string
	ClientID  string
	// ClientSecret is required for the browser (BFF) login flow. Bearer-token-only
	// deployments (headless CLI) can leave it unset.
	ClientSecret string
	// RedirectURL is the fully-qualified /auth/callback URL registered with the
	// IdP. Must match exactly. Required for the browser login flow.
	RedirectURL string
	// Audience defaults to ClientID if unset.
	Audience string
	// DevInsecureSkipAuth disables OIDC verification entirely. NEVER set this in
	// production — it hands every /api/* call full access.
	DevInsecureSkipAuth bool
}

type DBConfig struct {
	// Driver is "sqlite" or "postgres". Operator picks at deploy time.
	Driver string
	// DSN is a sqlite file path (e.g. "/var/lib/spinup/spinup.db")
	// or a postgres connection string ("postgres://user:pass@host/db?sslmode=require").
	DSN string
}

func Load() (Config, error) {
	c := Config{
		HTTP: HTTPConfig{
			Addr: env("SPINUP_HTTP_ADDR", ":8080"),
		},
		OIDC: OIDCConfig{
			IssuerURL:           env("SPINUP_OIDC_ISSUER_URL", ""),
			ClientID:            env("SPINUP_OIDC_CLIENT_ID", ""),
			ClientSecret:        env("SPINUP_OIDC_CLIENT_SECRET", ""),
			RedirectURL:         env("SPINUP_OIDC_REDIRECT_URL", ""),
			Audience:            env("SPINUP_OIDC_AUDIENCE", ""),
			DevInsecureSkipAuth: env("SPINUP_DEV_INSECURE_SKIP_AUTH", "") == "true",
		},
		Authz: AuthzConfig{
			RequiredRoles: splitCSV(env("SPINUP_AUTHZ_REQUIRED_ROLES", "")),
		},
		DB: DBConfig{
			Driver: strings.ToLower(env("SPINUP_DB_DRIVER", "sqlite")),
			DSN:    env("SPINUP_DB_DSN", "spinup.db"),
		},
		Functions: FunctionsConfig{
			Namespace:     env("SPINUP_FUNCTIONS_NAMESPACE", "spinup-functions"),
			PublicBaseURL: strings.TrimRight(env("SPINUP_PUBLIC_BASE_URL", ""), "/"),
			PublicDomain:  strings.TrimSpace(env("SPINUP_FUNCTIONS_PUBLIC_DOMAIN", "")),
			PublicGateway: strings.TrimSpace(env("SPINUP_FUNCTIONS_PUBLIC_GATEWAY", "")),
		},
		K8s: K8sConfig{
			Kubeconfig: env("SPINUP_KUBECONFIG", ""),
		},
		Builder: BuilderConfig{
			GoImage:     env("SPINUP_BUILDER_IMAGE_GO", "spinup/builder-go:latest"),
			JSImage:     env("SPINUP_BUILDER_IMAGE_JS", "spinup/builder-js:latest"),
			TSImage:     env("SPINUP_BUILDER_IMAGE_TS", "spinup/builder-ts:latest"),
			RustImage:   env("SPINUP_BUILDER_IMAGE_RUST", "spinup/builder-rust:latest"),
			RegistryURL: env("SPINUP_OCI_REGISTRY_URL", "ttl.sh/spinup"),
			AuthSecret:  env("SPINUP_OCI_AUTH_SECRET", ""),
		},
		Metrics: MetricsConfig{
			PrometheusURL: strings.TrimRight(env("SPINUP_PROMETHEUS_URL", ""), "/"),
		},
		Worker: WorkerConfig{
			URL:   strings.TrimRight(env("SPINUP_WORKER_URL", ""), "/"),
			UIURL: strings.TrimRight(env("SPINUP_WORKER_UI_URL", ""), "/"),
		},
		UI: UIConfig{
			StaticDir: env("SPINUP_UI_STATIC_DIR", ""),
		},
	}

	if !c.OIDC.DevInsecureSkipAuth {
		if c.OIDC.IssuerURL == "" || c.OIDC.ClientID == "" {
			return Config{}, fmt.Errorf("SPINUP_OIDC_ISSUER_URL and SPINUP_OIDC_CLIENT_ID are required (or set SPINUP_DEV_INSECURE_SKIP_AUTH=true for local dev)")
		}
	}
	if c.DB.Driver != "sqlite" && c.DB.Driver != "postgres" {
		return Config{}, fmt.Errorf("SPINUP_DB_DRIVER must be sqlite or postgres, got %q", c.DB.Driver)
	}

	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
