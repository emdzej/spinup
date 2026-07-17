package store

import (
	"context"
	"fmt"
	"time"

	"github.com/emdzej/spinup/services/control-plane/internal/config"
)

// Runtime picks how a deployed Application is served on the cluster.
type Runtime string

const (
	// RuntimeSpinKube (default) uses the SpinKube operator: one SpinApp CR /
	// Deployment / pod per Application, always-on.
	RuntimeSpinKube Runtime = "spinkube"
	// RuntimeWorkerPool routes traffic through the shared spinup-worker pool:
	// the app is not a K8s object; the worker spawns / caches a Spin process
	// on demand. Cold start on first request, no per-app pod cost.
	RuntimeWorkerPool Runtime = "workerpool"
)

// Application is the top-level user-facing unit. It carries 1+ Functions, each
// a component + HTTP trigger route. Its Runtime determines how it's served.
type Application struct {
	ID          string
	TenantID    string
	Name        string // DNS-1123, unique per tenant; used as the SpinApp resource name
	Language    string // "go" | "js" | "ts" | "rust" — all functions in an app share this
	Runtime     Runtime
	Description string
	// Replicas is the desired pod count for the deployed SpinApp. 0 or 1 both
	// mean single-replica; >1 requires the workload to be stateless (Spin
	// functions are, by design).
	Replicas int32
	// Variables are Spin's per-app variables (accessible via the SDK's
	// variables API from any function). V1 supports literal values only;
	// Secret/ConfigMap sourcing is a natural follow-up.
	Variables []Variable
	// Resources scopes the deployed pod's CPU/memory requests + limits.
	// Empty strings mean "don't set that field" (BestEffort QoS).
	Resources Resources
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Variable is one entry in the SpinApp's spec.variables[]. Literal value only
// for now — the CR also allows valueFrom (Secret / ConfigMap) but that's a
// v2 feature.
type Variable struct {
	Name  string
	Value string
}

// Resources maps 1:1 to k8s ResourceRequirements. Values are k8s quantity
// strings ("100m", "128Mi"); empty = unset (BestEffort).
type Resources struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

// Function is a component within an Application. Multiple functions share one
// wasmtime process (one pod) but each has its own WASM binary + trigger route.
type Function struct {
	ID            string
	ApplicationID string
	Name          string // component name; unique within the app; DNS-1123
	Route         string // Spin trigger route, e.g. "/..." or "/hello/..."
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Source is the current user-provided source tree for a Function.
// Keys are relative paths (e.g. "main.go", "src/index.ts", "helper.go").
type Source struct {
	FunctionID string
	Files      map[string]string
	UpdatedAt  time.Time
}

type BuildStatus string

const (
	BuildPending   BuildStatus = "pending"
	BuildRunning   BuildStatus = "running"
	BuildSucceeded BuildStatus = "succeeded"
	BuildFailed    BuildStatus = "failed"
)

// Build is per-Application (one OCI image per build, packing all its Functions).
type Build struct {
	ID            string
	ApplicationID string
	ImageRef      string
	// ImageSizeBytes is the sum of config.size + layer sizes from the OCI
	// manifest (compressed on-wire size). Nil for older builds that predate
	// the size-reporting logic in the builder entrypoint.
	ImageSizeBytes *int64
	Status         BuildStatus
	Error          string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

// Store is the persistence contract.
type Store interface {
	Ping(ctx context.Context) error

	// Applications
	ListApplications(ctx context.Context, tenantID string) ([]Application, error)
	GetApplication(ctx context.Context, tenantID, id string) (Application, error)
	GetApplicationByName(ctx context.Context, tenantID, name string) (Application, error)
	CreateApplication(ctx context.Context, a Application) error
	// UpdateApplicationConfig updates the mutable app-level knobs: description,
	// replicas, variables, resources. Name/language/runtime are immutable.
	UpdateApplicationConfig(ctx context.Context, tenantID, id string, desc string, replicas int32, variables []Variable, resources Resources) error
	DeleteApplication(ctx context.Context, tenantID, id string) error

	// Functions (nested)
	ListFunctions(ctx context.Context, applicationID string) ([]Function, error)
	GetFunction(ctx context.Context, applicationID, id string) (Function, error)
	CreateFunction(ctx context.Context, f Function) error
	UpdateFunctionRoute(ctx context.Context, applicationID, id, route string) error
	DeleteFunction(ctx context.Context, applicationID, id string) error

	// Source (per Function)
	GetSource(ctx context.Context, functionID string) (Source, error)
	PutSource(ctx context.Context, s Source) error

	// Builds (per Application)
	CreateBuild(ctx context.Context, b Build) error
	GetBuild(ctx context.Context, applicationID, buildID string) (Build, error)
	ListBuilds(ctx context.Context, applicationID string, limit int) ([]Build, error)
	UpdateBuildStatus(ctx context.Context, buildID string, status BuildStatus, errMsg string, finished *time.Time) error
	UpdateBuildImageSize(ctx context.Context, buildID string, sizeBytes int64) error

	Close() error
}

var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "not found" }

func Open(ctx context.Context, cfg config.DBConfig) (Store, error) {
	switch cfg.Driver {
	case "sqlite":
		return openSQLite(ctx, cfg.DSN)
	case "postgres":
		return nil, fmt.Errorf("postgres driver not yet implemented")
	default:
		return nil, fmt.Errorf("unknown db driver %q", cfg.Driver)
	}
}
