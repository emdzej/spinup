//! spinup-worker — multi-tenant wasmtime host.
//!
//! One process, N Spin applications loaded on demand. Each incoming request
//! is dispatched to the correct application by path prefix
//! (`/apps/{name}/{rest}`), then to the matching component by Spin trigger
//! route, then invoked via WASI HTTP.

use std::sync::Arc;

use anyhow::Context;
use tracing::info;
use tracing_subscriber::EnvFilter;

mod config;
mod loader;
mod router;
mod runtime;
mod server;

#[derive(Debug, Clone)]
pub struct WorkerConfig {
    /// Address to bind the HTTP server, e.g. "0.0.0.0:8000".
    pub listen_addr: String,
    /// Base URL of the control plane. If set, we poll for the app catalog.
    pub control_plane_url: Option<String>,
    /// Bearer token used against the control plane's OIDC-gated API.
    /// Empty when the control plane runs with SPINUP_DEV_INSECURE_SKIP_AUTH=true.
    pub control_plane_token: Option<String>,
    /// Poll interval for the control plane config.
    pub poll_interval_secs: u64,
    /// Directory used by `spin registry pull` for cached artifacts.
    pub cache_dir: String,
}

impl WorkerConfig {
    fn from_env() -> Self {
        Self {
            listen_addr: std::env::var("SPINUP_WORKER_ADDR").unwrap_or_else(|_| "0.0.0.0:8000".into()),
            control_plane_url: std::env::var("SPINUP_CONTROL_PLANE_URL").ok(),
            control_plane_token: std::env::var("SPINUP_CONTROL_PLANE_TOKEN").ok(),
            poll_interval_secs: std::env::var("SPINUP_POLL_INTERVAL_SECS")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(10),
            cache_dir: std::env::var("SPINUP_CACHE_DIR").unwrap_or_else(|_| "/var/lib/spinup-worker".into()),
        }
    }
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")))
        .init();

    let cfg = WorkerConfig::from_env();
    info!(?cfg, "spinup-worker starting");

    // Warm up the on-disk cache directory.
    std::fs::create_dir_all(&cfg.cache_dir)
        .with_context(|| format!("create cache dir {}", cfg.cache_dir))?;

    let router = Arc::new(router::Router::new(cfg.clone()).await?);

    // Kick off the config poller (no-op if no control plane URL is set).
    if cfg.control_plane_url.is_some() {
        let r = router.clone();
        let cfg = cfg.clone();
        tokio::spawn(async move {
            if let Err(e) = config::poll_loop(cfg, r).await {
                tracing::error!(error = %e, "config poller exited");
            }
        });
    } else {
        info!("SPINUP_CONTROL_PLANE_URL not set — worker will only serve manually-registered apps");
    }

    server::run(cfg.listen_addr.clone(), router).await
}
