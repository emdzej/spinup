package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	db *sql.DB
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS applications (
	id          TEXT PRIMARY KEY,
	tenant_id   TEXT NOT NULL,
	name        TEXT NOT NULL,
	language    TEXT NOT NULL,
	runtime     TEXT NOT NULL DEFAULT 'spinkube',
	description TEXT NOT NULL DEFAULT '',
	created_at  DATETIME NOT NULL,
	updated_at  DATETIME NOT NULL,
	UNIQUE(tenant_id, name)
);

CREATE TABLE IF NOT EXISTS functions (
	id             TEXT PRIMARY KEY,
	application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
	name           TEXT NOT NULL,
	route          TEXT NOT NULL,
	created_at     DATETIME NOT NULL,
	updated_at     DATETIME NOT NULL,
	UNIQUE(application_id, name)
);
CREATE INDEX IF NOT EXISTS idx_functions_app ON functions(application_id);

CREATE TABLE IF NOT EXISTS sources (
	function_id TEXT PRIMARY KEY REFERENCES functions(id) ON DELETE CASCADE,
	files       TEXT NOT NULL,
	updated_at  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS builds (
	id                TEXT PRIMARY KEY,
	application_id    TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
	image_ref         TEXT NOT NULL,
	image_size_bytes  INTEGER,
	status            TEXT NOT NULL,
	error             TEXT NOT NULL DEFAULT '',
	created_at        DATETIME NOT NULL,
	finished_at       DATETIME
);
CREATE INDEX IF NOT EXISTS idx_builds_app_created ON builds(application_id, created_at DESC);

-- Idempotent additive migration for existing DBs from before the column landed.
-- SQLite doesn't have ALTER TABLE ... ADD COLUMN IF NOT EXISTS, so we just try
-- and swallow the "duplicate column" error via the check below at runtime.
`

func openSQLite(ctx context.Context, dsn string) (Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, sqliteSchema); err != nil {
		return nil, fmt.Errorf("apply sqlite schema: %w", err)
	}
	// Additive migration for pre-existing DBs. Ignore "duplicate column" errors.
	if _, err := db.ExecContext(ctx, `ALTER TABLE builds ADD COLUMN image_size_bytes INTEGER`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return nil, fmt.Errorf("migrate builds.image_size_bytes: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *sqliteStore) Close() error                   { return s.db.Close() }

// --- Applications ---

func (s *sqliteStore) ListApplications(ctx context.Context, tenantID string) ([]Application, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, language, runtime, description, created_at, updated_at
		FROM applications WHERE tenant_id = ? ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		var a Application
		if err := rows.Scan(&a.ID, &a.TenantID, &a.Name, &a.Language, (*string)(&a.Runtime), &a.Description, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetApplication(ctx context.Context, tenantID, id string) (Application, error) {
	var a Application
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, language, runtime, description, created_at, updated_at
		FROM applications WHERE tenant_id = ? AND id = ?`, tenantID, id).
		Scan(&a.ID, &a.TenantID, &a.Name, &a.Language, (*string)(&a.Runtime), &a.Description, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	return a, err
}

func (s *sqliteStore) GetApplicationByName(ctx context.Context, tenantID, name string) (Application, error) {
	var a Application
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, language, runtime, description, created_at, updated_at
		FROM applications WHERE tenant_id = ? AND name = ?`, tenantID, name).
		Scan(&a.ID, &a.TenantID, &a.Name, &a.Language, (*string)(&a.Runtime), &a.Description, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	return a, err
}

func (s *sqliteStore) CreateApplication(ctx context.Context, a Application) error {
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Runtime == "" {
		a.Runtime = RuntimeSpinKube
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO applications (id, tenant_id, name, language, runtime, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.TenantID, a.Name, a.Language, string(a.Runtime), a.Description, a.CreatedAt, a.UpdatedAt)
	return err
}

func (s *sqliteStore) DeleteApplication(ctx context.Context, tenantID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM applications WHERE tenant_id = ? AND id = ?`, tenantID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Functions ---

func (s *sqliteStore) ListFunctions(ctx context.Context, applicationID string) ([]Function, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, application_id, name, route, created_at, updated_at
		FROM functions WHERE application_id = ? ORDER BY name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Function
	for rows.Next() {
		var f Function
		if err := rows.Scan(&f.ID, &f.ApplicationID, &f.Name, &f.Route, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetFunction(ctx context.Context, applicationID, id string) (Function, error) {
	var f Function
	err := s.db.QueryRowContext(ctx, `
		SELECT id, application_id, name, route, created_at, updated_at
		FROM functions WHERE application_id = ? AND id = ?`, applicationID, id).
		Scan(&f.ID, &f.ApplicationID, &f.Name, &f.Route, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Function{}, ErrNotFound
	}
	return f, err
}

func (s *sqliteStore) CreateFunction(ctx context.Context, f Function) error {
	now := time.Now().UTC()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO functions (id, application_id, name, route, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		f.ID, f.ApplicationID, f.Name, f.Route, f.CreatedAt, f.UpdatedAt)
	return err
}

func (s *sqliteStore) UpdateFunctionRoute(ctx context.Context, applicationID, id, route string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE functions SET route = ?, updated_at = ?
		WHERE application_id = ? AND id = ?`, route, now, applicationID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) DeleteFunction(ctx context.Context, applicationID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM functions WHERE application_id = ? AND id = ?`, applicationID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Source ---

func (s *sqliteStore) GetSource(ctx context.Context, functionID string) (Source, error) {
	var (
		src      Source
		filesRaw string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT function_id, files, updated_at
		FROM sources WHERE function_id = ?`, functionID).
		Scan(&src.FunctionID, &filesRaw, &src.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, ErrNotFound
	}
	if err != nil {
		return Source{}, err
	}
	if err := json.Unmarshal([]byte(filesRaw), &src.Files); err != nil {
		return Source{}, fmt.Errorf("decode source files: %w", err)
	}
	return src, nil
}

func (s *sqliteStore) PutSource(ctx context.Context, src Source) error {
	src.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(src.Files)
	if err != nil {
		return fmt.Errorf("encode source files: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sources (function_id, files, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(function_id) DO UPDATE SET files = excluded.files, updated_at = excluded.updated_at`,
		src.FunctionID, string(raw), src.UpdatedAt)
	return err
}

// --- Builds ---

func (s *sqliteStore) CreateBuild(ctx context.Context, b Build) error {
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO builds (id, application_id, image_ref, status, error, created_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.ApplicationID, b.ImageRef, string(b.Status), b.Error, b.CreatedAt, b.FinishedAt)
	return err
}

func (s *sqliteStore) GetBuild(ctx context.Context, applicationID, buildID string) (Build, error) {
	var b Build
	var status string
	var size sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, application_id, image_ref, image_size_bytes, status, error, created_at, finished_at
		FROM builds WHERE application_id = ? AND id = ?`, applicationID, buildID).
		Scan(&b.ID, &b.ApplicationID, &b.ImageRef, &size, &status, &b.Error, &b.CreatedAt, &b.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Build{}, ErrNotFound
	}
	if size.Valid {
		b.ImageSizeBytes = &size.Int64
	}
	b.Status = BuildStatus(status)
	return b, err
}

func (s *sqliteStore) ListBuilds(ctx context.Context, applicationID string, limit int) ([]Build, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, application_id, image_ref, image_size_bytes, status, error, created_at, finished_at
		FROM builds WHERE application_id = ? ORDER BY created_at DESC LIMIT ?`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Build
	for rows.Next() {
		var b Build
		var status string
		var size sql.NullInt64
		if err := rows.Scan(&b.ID, &b.ApplicationID, &b.ImageRef, &size, &status, &b.Error, &b.CreatedAt, &b.FinishedAt); err != nil {
			return nil, err
		}
		if size.Valid {
			b.ImageSizeBytes = &size.Int64
		}
		b.Status = BuildStatus(status)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateBuildStatus(ctx context.Context, buildID string, status BuildStatus, errMsg string, finished *time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE builds SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		string(status), errMsg, finished, buildID)
	return err
}

func (s *sqliteStore) UpdateBuildImageSize(ctx context.Context, buildID string, sizeBytes int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE builds SET image_size_bytes = ? WHERE id = ?`,
		sizeBytes, buildID)
	return err
}
