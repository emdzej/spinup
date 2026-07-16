//! OCI pull + Spin lock-file parse.
//!
//! We shell out to `spin registry pull` for the transport-layer work — it
//! populates the standard Spin cache at `$HOME/.cache/spin/registry/`. We
//! then read Spin's own "lock file" (`config.json` under the manifest dir)
//! and resolve each component to its WASM blob under `wasm/{digest}`.
//!
//! The lock file is Spin's compiled view of a spin.toml — it lists
//! triggers, components, and component sources by digest. It's easier to
//! parse than reconstructing the source spin.toml.

use std::collections::HashMap;
use std::path::PathBuf;

use anyhow::{anyhow, Context, Result};
use serde::Deserialize;
use tracing::info;

/// A loaded Spin app: name + reference + per-component wasm paths + routes.
#[derive(Debug, Clone)]
pub struct LoadedApp {
    pub name: String,
    pub image_ref: String,
    /// Component id → info. Populated from the Spin lock file.
    pub components: HashMap<String, ComponentInfo>,
    /// Triggers in declaration order — used for deterministic route matching.
    pub triggers: Vec<HttpTrigger>,
}

#[derive(Debug, Clone)]
pub struct ComponentInfo {
    pub id: String,
    /// Absolute filesystem path to the WASM blob in the Spin cache.
    pub wasm_path: PathBuf,
}

#[derive(Debug, Clone)]
pub struct HttpTrigger {
    pub route: String,
    pub component: String,
}

// ---------- Spin lock file schema (subset) ----------

#[derive(Debug, Deserialize)]
struct SpinLock {
    #[serde(default)]
    triggers: Vec<LockTrigger>,
    #[serde(default)]
    components: Vec<LockComponent>,
}

#[derive(Debug, Deserialize)]
struct LockTrigger {
    #[allow(dead_code)]
    #[serde(default)]
    trigger_type: String,
    trigger_config: LockTriggerConfig,
}

#[derive(Debug, Deserialize)]
struct LockTriggerConfig {
    component: String,
    route: String,
}

#[derive(Debug, Deserialize)]
struct LockComponent {
    id: String,
    source: LockSource,
}

#[derive(Debug, Deserialize)]
struct LockSource {
    digest: String,
}

// ---------- OCI manifest schema (only what we need) ----------

impl LoadedApp {
    /// Pulls the given OCI ref via `spin registry pull`, then resolves each
    /// component to a wasm blob path under the Spin cache.
    pub async fn pull(cache_root: &std::path::Path, name: &str, image_ref: &str) -> Result<Self> {
        // Work dir here is just a scratch location for `spin registry pull`
        // to run in; the actual pulled artifacts go to $HOME/.cache/spin/registry/.
        let work_dir = cache_root.join(name);
        std::fs::create_dir_all(&work_dir).with_context(|| format!("mkdir {}", work_dir.display()))?;

        info!(app = name, image = image_ref, "pulling OCI image");
        let output = tokio::process::Command::new("spin")
            .args(["registry", "pull", "--insecure", image_ref])
            .current_dir(&work_dir)
            .output()
            .await
            .with_context(|| "spawn `spin registry pull`")?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(anyhow!("spin registry pull failed: {}", stderr));
        }

        // Locate the Spin cache root: $HOME/.cache/spin/registry.
        let cache_home = std::env::var("HOME")
            .map(PathBuf::from)
            .context("HOME not set")?
            .join(".cache/spin/registry");

        let (host, path, tag) = parse_ref(image_ref)?;
        let manifest_dir = cache_home.join("manifests").join(host).join(&path).join(tag);
        let lock_path = manifest_dir.join("config.json");
        let lock_text = std::fs::read_to_string(&lock_path)
            .with_context(|| format!("read {}", lock_path.display()))?;
        let lock: SpinLock = serde_json::from_str(&lock_text).context("parse spin lock config.json")?;

        // Resolve each component's wasm blob path.
        let mut components = HashMap::new();
        for c in &lock.components {
            let blob_path = cache_home.join("wasm").join(&c.source.digest);
            if !blob_path.exists() {
                return Err(anyhow!(
                    "component {} references digest {} but blob is missing at {}",
                    c.id,
                    c.source.digest,
                    blob_path.display()
                ));
            }
            components.insert(
                c.id.clone(),
                ComponentInfo {
                    id: c.id.clone(),
                    wasm_path: blob_path,
                },
            );
        }

        let triggers = lock
            .triggers
            .into_iter()
            .map(|t| HttpTrigger {
                route: t.trigger_config.route,
                component: t.trigger_config.component,
            })
            .collect();

        Ok(Self {
            name: name.to_string(),
            image_ref: image_ref.to_string(),
            components,
            triggers,
        })
    }

    /// Match a request path against this app's HTTP triggers.
    /// Returns the matching component id and the path stripped of the route prefix.
    pub fn resolve<'a>(&'a self, path: &str) -> Option<(&'a str, String)> {
        let mut triggers: Vec<&HttpTrigger> = self.triggers.iter().collect();
        triggers.sort_by_key(|t| std::cmp::Reverse(prefix_of(&t.route).len()));
        for t in triggers {
            if let Some(rest) = match_route(&t.route, path) {
                return Some((t.component.as_str(), rest));
            }
        }
        None
    }
}

/// Parse an image reference like "registry:5000/repo/name:tag" into
/// (host, path, tag).
fn parse_ref(image_ref: &str) -> Result<(String, String, String)> {
    // Split on ':' from the right, but only when the right side has no '/'.
    let (name_part, tag) = match image_ref.rsplit_once(':') {
        Some((n, t)) if !t.contains('/') => (n.to_string(), t.to_string()),
        _ => return Err(anyhow!("image ref missing tag: {}", image_ref)),
    };
    // Everything before the first '/' is the host if it looks like one
    // (contains a dot or a colon-port). Otherwise it's part of the path
    // and the host is docker.io by convention.
    let (host, path) = match name_part.split_once('/') {
        Some((h, rest)) if h.contains('.') || h.contains(':') || h.contains(':') => {
            (h.to_string(), rest.to_string())
        }
        _ => ("docker.io".to_string(), name_part.clone()),
    };
    Ok((host, path, tag))
}

fn prefix_of(route: &str) -> &str {
    if let Some(idx) = route.find("/...") {
        &route[..idx]
    } else {
        route
    }
}

fn match_route(route: &str, path: &str) -> Option<String> {
    if route.ends_with("/...") {
        let prefix = &route[..route.len() - 4];
        if prefix.is_empty() || path == prefix || path.starts_with(&format!("{}/", prefix)) {
            let rest = if prefix.is_empty() {
                path.to_string()
            } else {
                path[prefix.len()..].to_string()
            };
            let rest = if rest.is_empty() { "/".to_string() } else { rest };
            return Some(rest);
        }
        None
    } else if route == path {
        Some("/".to_string())
    } else {
        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn wildcard_matches_all() {
        assert_eq!(match_route("/...", "/foo/bar"), Some("/foo/bar".into()));
        assert_eq!(match_route("/...", "/"), Some("/".into()));
    }
    #[test]
    fn prefix_matches_children() {
        assert_eq!(match_route("/hello/...", "/hello/world"), Some("/world".into()));
        assert_eq!(match_route("/hello/...", "/hello"), Some("/".into()));
        assert_eq!(match_route("/hello/...", "/other"), None);
    }
    #[test]
    fn static_route_only_exact() {
        assert_eq!(match_route("/hello", "/hello"), Some("/".into()));
        assert_eq!(match_route("/hello", "/hello/x"), None);
    }
    #[test]
    fn parse_ref_with_port() {
        let (h, p, t) = parse_ref("172.19.0.2:5000/spinup/foo:abc").unwrap();
        assert_eq!(h, "172.19.0.2:5000");
        assert_eq!(p, "spinup/foo");
        assert_eq!(t, "abc");
    }
}
