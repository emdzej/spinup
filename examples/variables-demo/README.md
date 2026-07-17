# Variables demo

Two small functions that exercise Spinup's **app-level Variables** — key/value
config set once on the Application and readable from every function in the
app at request time.

| Function | Route | Reads |
|---|---|---|
| `greet` | `/greet/...` | `greeting` — used verbatim as the response body |
| `authed` | `/authed/...` | `api_key` — must match the `X-Api-Key` request header |

## Try it

1. Create an Application `vars` (TypeScript).
2. Create two Functions: `greet` and `authed` with the sources here.
3. On the app page, open **Configuration → Variables** and add:
   - `greeting` → `hello from cluster`
   - `api_key` → any random string
4. Also set `Resources` to something modest (e.g. `100m` / `500m` / `64Mi` / `256Mi`) and `Replicas: 2` while you're there.
5. Save → Build & Deploy.

```bash
curl https://vars.spinup.solvely.pl/greet
# hello from cluster

curl -H "x-api-key: <the value you set>" https://vars.spinup.solvely.pl/authed
# ok — authenticated

curl https://vars.spinup.solvely.pl/authed
# unauthorized  (401)
```

## What this proves

- **Variables** — set once on the app, no per-function config, no rebuild
  to rotate `api_key`. The shim reads variables at request time.
- **Replicas > 1** — the same request keeps working; the LB picks any pod.
- **Resources** — you'll see the value in `kubectl -n spinup-functions get pod
  <name> -o jsonpath='{.spec.containers[0].resources}'`.
