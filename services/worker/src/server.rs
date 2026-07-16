//! HTTP entry point: axum handler that dispatches inbound requests to the
//! right app + component + WASI HTTP invocation.

use std::sync::Arc;

use anyhow::Result;
use axum::body::Body;
use axum::extract::{Path, State};
use axum::http::{HeaderMap, Method, Response, StatusCode, Uri};
use axum::response::IntoResponse;
use axum::routing::{any, get};
use axum::Router as AxumRouter;
use bytes::Bytes;
use http_body_util::BodyExt;
use tracing::{error, info};

use crate::router::Router;

pub async fn run(addr: String, router: Arc<Router>) -> Result<()> {
    let app = AxumRouter::new()
        .route("/healthz", get(|| async { "ok" }))
        .route("/apps/{app}", any(dispatch_root))
        .route("/apps/{app}/{*rest}", any(dispatch_rest))
        .with_state(router);

    let listener = tokio::net::TcpListener::bind(&addr).await?;
    info!(addr = %addr, "worker HTTP server listening");
    axum::serve(listener, app).await?;
    Ok(())
}

async fn dispatch_root(
    State(router): State<Arc<Router>>,
    Path(app): Path<String>,
    method: Method,
    uri: Uri,
    headers: HeaderMap,
    body: Body,
) -> Response<Body> {
    dispatch(&router, &app, "/", method, uri, headers, body).await
}

async fn dispatch_rest(
    State(router): State<Arc<Router>>,
    Path((app, rest)): Path<(String, String)>,
    method: Method,
    uri: Uri,
    headers: HeaderMap,
    body: Body,
) -> Response<Body> {
    dispatch(&router, &app, &format!("/{}", rest), method, uri, headers, body).await
}

async fn dispatch(
    router: &Router,
    app_name: &str,
    inner_path: &str,
    method: Method,
    uri: Uri,
    headers: HeaderMap,
    body: Body,
) -> Response<Body> {
    let Some((app, components)) = router.get(app_name).await else {
        return (StatusCode::NOT_FOUND, format!("no such app: {}", app_name)).into_response();
    };

    let Some((component_name, stripped)) = app.resolve(inner_path) else {
        return (
            StatusCode::NOT_FOUND,
            format!("no route matched {} in app {}", inner_path, app_name),
        )
            .into_response();
    };

    let Some(component) = components.get(component_name) else {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("component {} not compiled", component_name),
        )
            .into_response();
    };

    // Collect the axum body into Bytes so we can hand a Full<Bytes> to the
    // wasi-http adapter. Cap at 8 MiB to avoid runaway allocations.
    let body_bytes: Bytes = match body.collect().await {
        Ok(collected) => collected.to_bytes(),
        Err(e) => {
            error!(error = %e, "reading request body");
            return (StatusCode::BAD_REQUEST, "invalid request body").into_response();
        }
    };
    if body_bytes.len() > 8 * 1024 * 1024 {
        return (StatusCode::PAYLOAD_TOO_LARGE, "body exceeds 8 MiB").into_response();
    }

    // Rebuild the URI so the guest sees a path relative to its trigger route.
    let query = uri.query().map(|q| format!("?{}", q)).unwrap_or_default();
    let guest_uri: http::Uri = match format!("{}{}", stripped, query).parse() {
        Ok(u) => u,
        Err(e) => {
            error!(error = %e, "invalid guest URI");
            return (StatusCode::INTERNAL_SERVER_ERROR, "uri assembly").into_response();
        }
    };

    let http_method = http::Method::from_bytes(method.as_str().as_bytes())
        .unwrap_or(http::Method::GET);

    match router
        .host()
        .invoke(component, http_method, guest_uri, headers, body_bytes)
        .await
    {
        Ok(resp) => {
            let (parts, guest_body) = resp.into_parts();
            let axum_body = Body::new(guest_body);
            Response::from_parts(parts, axum_body)
        }
        Err(e) => {
            error!(app = app_name, component = component_name, error = %e, "invoke failed");
            (
                StatusCode::BAD_GATEWAY,
                format!("guest invocation failed: {}", e),
            )
                .into_response()
        }
    }
}
