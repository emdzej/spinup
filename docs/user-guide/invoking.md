# Invoking functions

Three ways to hit a deployed function, in order of convenience.

## 1. From the UI (Try it out)

The function detail page has a **Try it out** card with a mini HTTP client. Set method, path, headers, body, click **Send**, see the response with status code, headers, and body preview.

Path prefix should match the function's route. For a route of `/...` (wildcard), anything works. For `/api/...`, use `/api/whatever`.

This path relays through the control plane, which then proxies to the pod.

## 2. Through the control plane API

```bash
curl -X POST "http://localhost:8080/api/v1/applications/$APP/functions/$FN/invoke" \
  -H 'content-type: application/json' \
  -d '{
    "method": "GET",
    "path": "/hello?name=world",
    "headers": { "user-agent": ["curl/8"] }
  }'
```

Response:

```json
{
  "status": 200,
  "headers": {"content-type": ["text/plain"], "content-length": ["7"]},
  "body": "Hello, world!\n",
  "bodyIsBase64": false,
  "truncated": false,
  "durationMs": 4
}
```

- `body` is base64-encoded (`bodyIsBase64: true`) when the response isn't valid UTF-8 or its content-type isn't textual.
- `truncated: true` means the response body exceeded the 1 MiB cap (`proxy.MaxResponseBody`).
- `durationMs` is measured control-plane-side, not inside the function.

This is what the UI's Try it out uses. It works regardless of whether the function is exposed externally via Ingress.

## 3. Directly to the pod

Each Application gets a K8s Service. You can port-forward or hit it from another pod:

```bash
# From your laptop
kubectl -n spinup-functions port-forward svc/greeter 8080:80
curl http://localhost:8080/hello?name=world

# From inside the cluster
curl http://greeter.spinup-functions.svc.cluster.local/hello?name=world
```

The Application page's **Invoke** card shows the exact port-forward command for the current app.

## Public routing

For external traffic, the Helm chart installs an Istio VirtualService that routes `dnsName` traffic to the Application:

```
GET https://spinup.example.com/fn/{app-name}/{fn-route-suffix}
   →  Istio Gateway
   →  VirtualService(spinup-functions.svc.cluster.local)
   →  SpinApp pod
```

Configure `dnsName` in Helm values, provision the TLS cert (cert-manager), and functions become publicly reachable at `/fn/{app-name}/...`.

If you don't run Istio, disable it (`istio.enabled=false`) and add your own Ingress / Gateway API resources pointing at the Application Services in `spinup-functions`.

## Timeouts and limits

- **Request body**: 1 MiB max (`proxy.MaxRequestBody` in the CP). Larger requests get a 413.
- **Response body**: 1 MiB max cap on the CP's captured buffer; excess is truncated (`truncated: true` in the response envelope).
- **Concurrency per pod**: bounded by Spin's own concurrency settings — check the [Spin docs](https://spinframework.dev/v3/) for the current defaults.

## Debugging failed invocations

If the response is `502 invoke failed:` or `503 service unavailable:`:

1. Check the pod is up: `kubectl get pods -n spinup-functions`
2. Read the pod logs: **Runtime logs** card on the function page, or `kubectl logs -n spinup-functions deploy/{app-name}`
3. Check readiness: `kubectl get svc {app-name} -n spinup-functions -o wide` — no endpoints means the pod isn't Ready yet
