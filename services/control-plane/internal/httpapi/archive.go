package httpapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

// Cap uploads to avoid runaway allocations; matches the builder's ceiling.
const maxSourceUpload = 8 << 20 // 8 MiB — source-code sized

// GET /api/v1/applications/{appId}/functions/{fnId}/source.tar.gz
// Streams the function's source as a gzipped tar. Filenames match the paths
// stored in Source.Files.
func (s *Server) exportSource(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	_, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	src, err := s.store.GetSource(r.Context(), fn.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "no source uploaded yet", http.StatusNotFound)
			return
		}
		s.logger.Error("get source for export", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "application/gzip")
	w.Header().Set("content-disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, fn.Name))
	if err := writeTarGz(w, src.Files); err != nil {
		s.logger.Error("write tar.gz", "err", err)
	}
}

// POST /api/v1/applications/{appId}/functions/{fnId}/source.tar.gz
// Replaces the function's source with the uploaded tar.gz body.
func (s *Server) importSource(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	_, fn, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}
	files, err := readTarGz(http.MaxBytesReader(w, r.Body, maxSourceUpload))
	if err != nil {
		http.Error(w, "invalid archive: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(files) == 0 {
		http.Error(w, "archive is empty", http.StatusBadRequest)
		return
	}
	if err := s.store.PutSource(r.Context(), store.Source{FunctionID: fn.ID, Files: files}); err != nil {
		s.logger.Error("put source from import", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sourceDTO{Files: files, UpdatedAt: time.Now().UTC()})
}

func writeTarGz(w io.Writer, files map[string]string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		clean := path.Clean("/" + name)
		if clean == "/" || strings.HasPrefix(clean, "/..") {
			return fmt.Errorf("invalid path %q", name)
		}
		content := files[name]
		if err := tw.WriteHeader(&tar.Header{
			Name:    strings.TrimPrefix(clean, "/"),
			Mode:    0o644,
			Size:    int64(len(content)),
			ModTime: time.Unix(0, 0),
		}); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func readTarGz(r io.Reader) (map[string]string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	files := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue // skip dirs, symlinks, etc.
		}
		clean := path.Clean(hdr.Name)
		if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
			return nil, fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, io.LimitReader(tr, maxSourceUpload+1)); err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		if buf.Len() > maxSourceUpload {
			return nil, fmt.Errorf("file %s exceeds size limit", hdr.Name)
		}
		files[clean] = buf.String()
	}
	return files, nil
}
