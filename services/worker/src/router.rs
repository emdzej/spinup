//! In-memory routing table: app name → LoadedApp + compiled components.
//!
//! Populated from the control plane's `worker-config` poll. Each entry keeps
//! the app's unpacked directory, spin.toml, and pre-compiled components.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;

use anyhow::Result;
use tokio::sync::RwLock;
use tracing::{info, warn};
use wasmtime::component::Component;

use crate::config::WorkerAppEntry;
use crate::loader::LoadedApp;
use crate::runtime::Host;
use crate::WorkerConfig;

pub struct Router {
    cfg: WorkerConfig,
    host: Host,
    apps: RwLock<HashMap<String, AppEntry>>,
    cache_root: PathBuf,
}

pub struct AppEntry {
    pub app: LoadedApp,
    /// component name → compiled wasmtime::Component
    pub components: HashMap<String, Arc<Component>>,
}

impl Router {
    pub async fn new(cfg: WorkerConfig) -> Result<Self> {
        let cache_root = PathBuf::from(&cfg.cache_dir);
        std::fs::create_dir_all(&cache_root)?;
        Ok(Self {
            cfg,
            host: Host::new()?,
            apps: RwLock::new(HashMap::new()),
            cache_root,
        })
    }

    pub fn host(&self) -> &Host { &self.host }

    /// Return a snapshot reference to the app entry if present.
    pub async fn get(&self, name: &str) -> Option<(LoadedApp, HashMap<String, Arc<Component>>)> {
        let guard = self.apps.read().await;
        guard.get(name).map(|e| (e.app.clone(), e.components.clone()))
    }

    /// Reconcile the in-memory catalog with what the control plane just sent.
    /// Adds new apps, removes deleted ones, replaces changed image refs.
    pub async fn sync(&self, want: Vec<WorkerAppEntry>) -> Result<()> {
        let want_by_name: HashMap<_, _> = want.iter().map(|w| (w.name.clone(), w.clone())).collect();

        // Additions / updates
        for w in want.iter() {
            let existing = { self.apps.read().await.get(&w.name).map(|e| e.app.image_ref.clone()) };
            if existing.as_deref() == Some(w.image_ref.as_str()) {
                continue; // unchanged
            }
            match self.pull_and_compile(&w.name, &w.image_ref).await {
                Ok(entry) => {
                    self.apps.write().await.insert(w.name.clone(), entry);
                    info!(app = %w.name, image = %w.image_ref, "app loaded");
                }
                Err(e) => warn!(app = %w.name, error = ?e, "failed to load app"),
            }
        }

        // Removals
        let names_to_remove: Vec<String> = {
            let guard = self.apps.read().await;
            guard.keys()
                .filter(|n| !want_by_name.contains_key(*n))
                .cloned()
                .collect()
        };
        if !names_to_remove.is_empty() {
            let mut guard = self.apps.write().await;
            for n in names_to_remove {
                guard.remove(&n);
                info!(app = %n, "app removed");
            }
        }

        Ok(())
    }

    async fn pull_and_compile(&self, name: &str, image_ref: &str) -> Result<AppEntry> {
        let app = LoadedApp::pull(&self.cache_root, name, image_ref).await?;

        let mut components = HashMap::new();
        for (comp_id, info) in &app.components {
            let compiled = self.host.compile(&info.wasm_path)?;
            components.insert(comp_id.clone(), Arc::new(compiled));
        }
        Ok(AppEntry { app, components })
    }

    #[allow(dead_code)]
    pub fn cfg(&self) -> &WorkerConfig { &self.cfg }
}
