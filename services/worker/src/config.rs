//! Periodic poll of the control plane's `/api/v1/worker-config` endpoint.
//!
//! Response shape must match `httpapi.workerConfigDTO` in the Go control
//! plane. Any change to that struct requires updating this deserialization.

use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context, Result};
use serde::Deserialize;
use tracing::{info, warn};

use crate::router::Router;
use crate::WorkerConfig;

#[derive(Debug, Deserialize)]
struct WorkerConfigDTO {
    #[serde(default)]
    apps: Vec<WorkerAppEntry>,
}

#[derive(Debug, Deserialize, Clone)]
pub struct WorkerAppEntry {
    pub id: String,
    pub name: String,
    pub language: String,
    #[serde(rename = "imageRef")]
    pub image_ref: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub functions: Vec<WorkerFunctionEntry>,
}

#[derive(Debug, Deserialize, Clone)]
pub struct WorkerFunctionEntry {
    pub name: String,
    pub route: String,
}

pub async fn poll_loop(cfg: WorkerConfig, router: Arc<Router>) -> Result<()> {
    let base = cfg
        .control_plane_url
        .as_ref()
        .expect("poll_loop called without control plane URL");
    let url = format!("{}/api/v1/worker-config", base.trim_end_matches('/'));

    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(10))
        .build()
        .context("build reqwest client")?;

    let mut interval = tokio::time::interval(Duration::from_secs(cfg.poll_interval_secs));
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        interval.tick().await;
        let mut req = client.get(&url);
        if let Some(tok) = &cfg.control_plane_token {
            req = req.bearer_auth(tok);
        }

        match req.send().await {
            Ok(resp) if resp.status().is_success() => match resp.json::<WorkerConfigDTO>().await {
                Ok(dto) => {
                    if let Err(e) = router.sync(dto.apps).await {
                        warn!(error = %e, "router sync failed");
                    }
                }
                Err(e) => warn!(error = %e, "decode worker-config"),
            },
            Ok(resp) => warn!(status = %resp.status(), "worker-config non-2xx"),
            Err(e) => warn!(error = %e, "worker-config poll failed"),
        }
        // First-loop hint only: don't spam.
        static ONCE: std::sync::Once = std::sync::Once::new();
        ONCE.call_once(|| info!("initial config poll complete"));
    }
}
