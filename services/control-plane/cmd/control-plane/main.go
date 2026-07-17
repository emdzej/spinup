package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emdzej/spinup/services/control-plane/internal/auth"
	"github.com/emdzej/spinup/services/control-plane/internal/builder"
	"github.com/emdzej/spinup/services/control-plane/internal/config"
	"github.com/emdzej/spinup/services/control-plane/internal/httpapi"
	"github.com/emdzej/spinup/services/control-plane/internal/istio"
	"github.com/emdzej/spinup/services/control-plane/internal/k8s"
	"github.com/emdzej/spinup/services/control-plane/internal/promql"
	"github.com/emdzej/spinup/services/control-plane/internal/proxy"
	"github.com/emdzej/spinup/services/control-plane/internal/session"
	"github.com/emdzej/spinup/services/control-plane/internal/spinapp"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
	"github.com/emdzej/spinup/services/control-plane/internal/telemetry"
	"github.com/emdzej/spinup/services/control-plane/internal/webui"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DB)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	verifier, err := auth.NewVerifier(ctx, cfg.OIDC)
	if err != nil {
		logger.Error("init oidc verifier", "err", err)
		os.Exit(1)
	}
	if cfg.OIDC.DevInsecureSkipAuth {
		logger.Warn("SPINUP_DEV_INSECURE_SKIP_AUTH=true — OIDC verification disabled, /api/* is wide open (dev only)")
	}

	// Server-side session store backs the BFF cookie flow. In-memory for now;
	// the Store interface lets us swap in a DB-backed impl later without
	// touching handlers.
	sessions := session.NewMemoryStore()
	verifier.SetSessions(sessions)

	// Authz: enforce that the ID token's `roles` claim contains one of the
	// configured required roles. Empty required list = every authenticated
	// user is allowed.
	verifier.SetPolicy(auth.NewPolicy(cfg.Authz.RequiredRoles))
	if len(cfg.Authz.RequiredRoles) > 0 {
		logger.Info("authz enforced", "required_roles", cfg.Authz.RequiredRoles)
	} else {
		logger.Info("authz disabled — any authenticated user is allowed (set SPINUP_AUTHZ_REQUIRED_ROLES to restrict)")
	}

	oauth, err := auth.NewOAuth(ctx, cfg.OIDC, sessions)
	if err != nil {
		logger.Error("init oidc oauth", "err", err)
		os.Exit(1)
	}

	kc, err := k8s.New(cfg.K8s.Kubeconfig)
	if err != nil {
		logger.Error("init k8s client", "err", err)
		os.Exit(1)
	}

	metrics, metricsHandler, err := telemetry.Init(ctx, "dev")
	if err != nil {
		logger.Error("init telemetry", "err", err)
		os.Exit(1)
	}

	spinClient := spinapp.New(kc.Dynamic, cfg.Functions.Namespace)
	// istio VirtualService client. Only used when PublicDomain + PublicGateway
	// are set (production ingress); otherwise the client is still constructed
	// but never called.
	vsClient := istio.New(kc.Dynamic, cfg.Functions.Namespace)
	if cfg.Functions.PublicDomain != "" && cfg.Functions.PublicGateway != "" {
		logger.Info("public function ingress enabled",
			"domain", cfg.Functions.PublicDomain, "gateway", cfg.Functions.PublicGateway)
	}
	buildRunner := builder.New(builder.Config{
		Logger:       logger,
		Kube:         kc.Typed,
		Store:        st,
		Spin:         spinClient,
		Namespace:    cfg.Functions.Namespace,
		GoImage:      cfg.Builder.GoImage,
		JSImage:      cfg.Builder.JSImage,
		TSImage:      cfg.Builder.TSImage,
		RustImage:    cfg.Builder.RustImage,
		RegistryURL:  cfg.Builder.RegistryURL,
		AuthSecret:                cfg.Builder.AuthSecret,
		ImagePullSecrets:          cfg.Builder.ImagePullSecrets,
		FunctionsImagePullSecrets: cfg.Functions.ImagePullSecrets,
		Metrics:                   metrics,
	})

	var promClient *promql.Client
	if cfg.Metrics.PrometheusURL != "" {
		promClient = promql.New(cfg.Metrics.PrometheusURL)
		logger.Info("prometheus client configured", "url", cfg.Metrics.PrometheusURL)
	}

	proxyClient, err := proxy.New(kc.Config)
	if err != nil {
		logger.Error("init service proxy", "err", err)
		os.Exit(1)
	}

	uiHandler := webui.Handler(cfg.UI.StaticDir)
	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           httpapi.New(logger, st, verifier, oauth, spinClient, vsClient, buildRunner, metrics, metricsHandler, cfg.Functions, promClient, proxyClient, cfg.Worker, uiHandler),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTP.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server exited", "err", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}
