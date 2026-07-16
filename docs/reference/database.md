# Database schema

Source of truth: `services/control-plane/internal/store/sqlite.go` (the same migrations run against Postgres). Reproduced here for reference.

## Tables

### `tenants`

Currently a single-row placeholder for future multi-tenancy.

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUID. Default row id is `default`. |
| `name` | TEXT | Display name. |
| `created_at` | TIMESTAMP | UTC. |

### `applications`

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUID. |
| `tenant_id` | TEXT FK → tenants.id | Currently always `default`. |
| `name` | TEXT UNIQUE | DNS-1123. Used as SpinApp CR name and OCI image name. |
| `language` | TEXT | `go` \| `js` \| `ts` \| `rust`. |
| `runtime` | TEXT | Currently only `spinkube`. Column reserved for future dense-packing runtimes on the scaling roadmap. |
| `description` | TEXT | Optional. |
| `created_at` | TIMESTAMP | UTC. |

### `functions`

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUID. |
| `application_id` | TEXT FK → applications.id ON DELETE CASCADE | |
| `name` | TEXT | DNS-1123. Unique per application (UNIQUE (application_id, name)). |
| `route` | TEXT | Spin route pattern, e.g. `/...`, `/api/...`, `/health`. |
| `created_at` | TIMESTAMP | UTC. |

### `sources`

Full source blob per Function, JSON-encoded `{filename: content}` map.

| Column | Type | Notes |
|---|---|---|
| `function_id` | TEXT PK, FK → functions.id ON DELETE CASCADE | |
| `files_json` | TEXT | JSON object. |
| `updated_at` | TIMESTAMP | UTC. Updated on every PUT /source. |

Reasonable for source sizes typical in HTTP functions (kilobytes to tens of kilobytes). For larger blobs, migrate to blob storage in a follow-up.

### `builds`

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | UUID **with dashes stripped** — also the OCI tag. |
| `application_id` | TEXT FK → applications.id ON DELETE CASCADE | |
| `status` | TEXT | `pending` \| `running` \| `succeeded` \| `failed`. |
| `image_ref` | TEXT | Full OCI ref including tag. Populated on `succeeded`. |
| `error` | TEXT | Populated on `failed`. |
| `created_at` | TIMESTAMP | UTC. |
| `finished_at` | TIMESTAMP | UTC. Null while `pending` or `running`. |

## Indexes

- `functions (application_id)` — for listing functions of an app.
- `builds (application_id, created_at DESC)` — for listing builds newest-first on the app page.
- `sources` uses `function_id` as its primary key (no additional index needed).

## Migrations

The control plane runs the DDL as an idempotent script at startup. No versioned migration tool (goose/atlas) is wired yet — schema additions happen by adding an `ALTER TABLE IF NOT EXISTS` block to `sqlite.go`.

When you add a column:

1. Add it to the DDL in `sqlite.go` and `postgres.go` (both files kept in sync).
2. Add a compatibility handler if the column can be null on existing rows.
3. Update any struct in `store/store.go` that maps to the row.
4. If the column changes existing API responses, update the DTOs in `internal/httpapi/*.go`.

## Backups

- **SQLite** — snapshot the mounted PVC (`db.sqlite.persistence`) via `kubectl cp` or a K8s snapshot controller. The DB is small (typically < 10 MB).
- **Postgres** — use your provider's snapshot / pg_dump story.

Function source is *inside* this DB — losing it means losing user code. Set up backups before real users touch it.

## Query patterns

The control plane's queries are small — no ORM, hand-written SQL in `store/sqlite.go`. Common patterns:

```sql
-- List applications for a tenant, latest first
SELECT id, name, language, runtime, description, created_at
FROM applications
WHERE tenant_id = ?
ORDER BY created_at DESC;

-- Get application with its functions
SELECT * FROM applications WHERE id = ?;
SELECT id, name, route FROM functions WHERE application_id = ? ORDER BY name;

-- Latest successful build
SELECT id, image_ref FROM builds
WHERE application_id = ? AND status = 'succeeded'
ORDER BY finished_at DESC
LIMIT 1;
```
