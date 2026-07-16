//! wasmtime host + WASI HTTP invocation.
//!
//! One `Engine` shared across all apps (compiled modules amortize over
//! process lifetime). Each request gets a fresh `Store` — wasmtime's per-
//! instance isolation model.
//!
//! wasmtime 46 API: WASI + wasi-http live under `::p2::` submodules;
//! `WasiView::ctx()` returns `WasiCtxView<'_>` bundling both `ctx` and
//! `table`; `WasiHttpView::http()` returns `WasiHttpCtxView<'_>` with
//! `ctx`, `table`, `hooks`.

use std::convert::Infallible;
use std::path::Path;
use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context as _, Result};
use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use wasmtime::component::{Component, Linker, ResourceTable};
use wasmtime::{Config, Engine, Store};
use wasmtime_wasi::{WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};
use wasmtime_wasi_http::p2::bindings::http::types::ErrorCode;
use wasmtime_wasi_http::p2::bindings::ProxyPre;
use wasmtime_wasi_http::p2::body::HyperOutgoingBody;
use wasmtime_wasi_http::p2::{WasiHttpCtxView, WasiHttpView};
use wasmtime_wasi_http::WasiHttpCtx;

/// Shared wasmtime engine + linker. Cheap to clone (Arc-ed internally).
#[derive(Clone)]
pub struct Host {
    engine: Engine,
    linker: Arc<Linker<HostState>>,
}

impl Host {
    pub fn new() -> Result<Self> {
        let mut cfg = Config::new();
        cfg.wasm_component_model_async(true);
        cfg.epoch_interruption(true);

        let engine = Engine::new(&cfg)?;
        let mut linker = Linker::<HostState>::new(&engine);
        // Link BOTH preview 2 and preview 3 imports so guests targeting either
        // version resolve. Spin v4's Go SDK targets wasi:http@0.3.x (preview 3
        // release-candidate).
        wasmtime_wasi::p2::add_to_linker_async(&mut linker)?;
        wasmtime_wasi::p3::add_to_linker(&mut linker)?;
        wasmtime_wasi_http::p2::add_only_http_to_linker_async(&mut linker)?;
        wasmtime_wasi_http::p3::add_to_linker(&mut linker)?;

        Ok(Self {
            engine,
            linker: Arc::new(linker),
        })
    }

    /// Compile a component from a `.wasm` file on disk. Callers cache the
    /// returned `Component` — compilation is expensive; instantiation is cheap.
    pub fn compile(&self, wasm_path: &Path) -> Result<Component> {
        let bytes = std::fs::read(wasm_path)
            .with_context(|| format!("read wasm {}", wasm_path.display()))?;
        // wasmtime::Error doesn't implement std::error::Error the way anyhow
        // expects, so route through map_err(anyhow::Error::from).
        Component::new(&self.engine, &bytes)
            .map_err(anyhow::Error::from)
            .with_context(|| format!("Component::new({}, len={})", wasm_path.display(), bytes.len()))
    }

    /// Invoke a compiled component's `wasi:http/incoming-handler` export.
    ///
    /// wasmtime 46 accepts any `Body<Data = Bytes>` whose `Error: Into<ErrorCode>`,
    /// so we can hand it a `Full<Bytes>` (`Error = Infallible`) directly —
    /// `Infallible` converts to any type since it's uninhabited.
    pub async fn invoke(
        &self,
        component: &Component,
        method: http::Method,
        uri: http::Uri,
        headers: http::HeaderMap,
        body: Bytes,
    ) -> Result<hyper::Response<HyperOutgoingBody>> {
        let state = Inner::default();
        let mut store = Store::new(&self.engine, state);
        store.set_epoch_deadline(1);

        let pre = ProxyPre::new(
            self.linker
                .instantiate_pre(component)
                .map_err(anyhow::Error::from)?,
        )
        .map_err(anyhow::Error::from)?;

        // Wrap Full<Bytes> (Error = Infallible) so its error maps into ErrorCode.
        // Since Infallible has no inhabitants, the closure is unreachable —
        // this is a compile-time cast.
        let mut req_builder = hyper::Request::builder().method(method).uri(uri);
        if let Some(hdr_mut) = req_builder.headers_mut() {
            *hdr_mut = headers;
        }
        let request_body = Full::new(body)
            .map_err(|i: Infallible| -> ErrorCode { match i {} })
            .boxed();
        let hyper_req = req_builder
            .body(request_body)
            .context("build hyper request")?;

        let request = store
            .data_mut()
            .http()
            .new_incoming_request(
                wasmtime_wasi_http::p2::bindings::http::types::Scheme::Http,
                hyper_req,
            )
            .map_err(anyhow::Error::from)
            .context("new_incoming_request")?;
        let (sender, receiver) = tokio::sync::oneshot::channel();
        let out = store
            .data_mut()
            .http()
            .new_response_outparam(sender)
            .map_err(anyhow::Error::from)
            .context("new_response_outparam")?;

        // Guard the guest with an epoch-based deadline. One-shot bump after
        // ~10s ends any runaway invocation.
        let engine = self.engine.clone();
        let tick = tokio::spawn(async move {
            tokio::time::sleep(Duration::from_secs(10)).await;
            engine.increment_epoch();
        });

        let handle_task = tokio::spawn(async move {
            let proxy = pre
                .instantiate_async(&mut store)
                .await
                .map_err(anyhow::Error::from)?;
            proxy
                .wasi_http_incoming_handler()
                .call_handle(&mut store, request, out)
                .await
                .map_err(anyhow::Error::from)
        });

        let result = match receiver.await {
            Ok(Ok(resp)) => Ok(resp),
            Ok(Err(code)) => Err(anyhow::anyhow!("guest error: {:?}", code)),
            Err(_) => match handle_task.await {
                Ok(Ok(())) => Err(anyhow::anyhow!("guest returned without producing a response")),
                Ok(Err(e)) => Err(e).context("guest call_handle"),
                Err(join_err) => Err(anyhow::anyhow!("handler task panicked: {}", join_err)),
            },
        };
        tick.abort();
        result
    }
}

pub type HostState = Inner;

pub struct Inner {
    table: ResourceTable,
    wasi: WasiCtx,
    http: WasiHttpCtx,
}

impl Default for Inner {
    fn default() -> Self {
        let wasi = WasiCtxBuilder::new()
            .inherit_stdout()
            .inherit_stderr()
            .build();
        Self {
            table: ResourceTable::new(),
            wasi,
            http: WasiHttpCtx::new(),
        }
    }
}

impl WasiView for Inner {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

impl WasiHttpView for Inner {
    fn http(&mut self) -> WasiHttpCtxView<'_> {
        // Empty hooks — the crate provides `impl WasiHttpHooks for [(); 0]`
        // as the default no-op implementation.
        static mut EMPTY_HOOKS: [(); 0] = [];
        // SAFETY: per-instance mutable static of a zero-sized type — no
        // aliasing, no data race, no observable state.
        let hooks: &mut dyn wasmtime_wasi_http::p2::WasiHttpHooks =
            unsafe { &mut *std::ptr::addr_of_mut!(EMPTY_HOOKS) };
        WasiHttpCtxView {
            ctx: &mut self.http,
            table: &mut self.table,
            hooks,
        }
    }
}

impl wasmtime_wasi_http::p3::WasiHttpView for Inner {
    fn http(&mut self) -> wasmtime_wasi_http::p3::WasiHttpCtxView<'_> {
        static mut EMPTY_HOOKS: [(); 0] = [];
        let hooks: &mut dyn wasmtime_wasi_http::p3::WasiHttpHooks =
            unsafe { &mut *std::ptr::addr_of_mut!(EMPTY_HOOKS) };
        wasmtime_wasi_http::p3::WasiHttpCtxView {
            ctx: &mut self.http,
            table: &mut self.table,
            hooks,
        }
    }
}

// Silence "unused import" — `ErrorCode` may only appear in error trait
// bounds inferred at monomorphization time.
#[allow(dead_code)]
fn _mark_error_code(_e: ErrorCode) {}
#[allow(dead_code)]
pub fn _mark_bytes(_b: Bytes) {}
