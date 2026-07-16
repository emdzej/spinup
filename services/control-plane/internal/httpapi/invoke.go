package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emdzej/spinup/services/control-plane/internal/proxy"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

type invokeRequestDTO struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   map[string][]string `json:"query,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	// Body is either a UTF-8 string or (rarely) base64-encoded when BodyIsBase64=true.
	Body         string `json:"body,omitempty"`
	BodyIsBase64 bool   `json:"bodyIsBase64,omitempty"`
}

type invokeResponseDTO struct {
	Status       int                 `json:"status"`
	Headers      map[string][]string `json:"headers"`
	Body         string              `json:"body"`
	BodyIsBase64 bool                `json:"bodyIsBase64"`
	Truncated    bool                `json:"truncated"`
	DurationMs   int64               `json:"durationMs"`
}

// POST /api/v1/applications/{appId}/functions/{fnId}/invoke
// Relays the request to http://{app-name}.{ns}.svc.cluster.local via the K8s
// API server's proxy. Spin's internal router matches the function's trigger
// route inside the pod based on request path.
func (s *Server) invokeFunction(w http.ResponseWriter, r *http.Request) {
	if !authed(r) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	app, _, ok := s.loadFunction(w, r, r.PathValue("appId"), r.PathValue("fnId"))
	if !ok {
		return
	}

	var in invokeRequestDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, proxy.MaxRequestBody+4096)).Decode(&in); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	var body []byte
	if in.Body != "" {
		if in.BodyIsBase64 {
			b, err := base64.StdEncoding.DecodeString(in.Body)
			if err != nil {
				http.Error(w, "invalid base64 body", http.StatusBadRequest)
				return
			}
			body = b
		} else {
			body = []byte(in.Body)
		}
		if len(body) > proxy.MaxRequestBody {
			http.Error(w, "body exceeds 1 MiB limit", http.StatusRequestEntityTooLarge)
			return
		}
	}

	if app.Runtime == store.RuntimeWorkerPool {
		if s.worker.invokeURL == "" {
			http.Error(w, "workerpool runtime not configured (SPINUP_WORKER_URL unset)", http.StatusServiceUnavailable)
			return
		}
		res, err := s.invokeWorker(r.Context(), app, in, body)
		if err != nil {
			s.logger.Warn("invoke", "err", err, "application", app.Name, "runtime", "workerpool")
			http.Error(w, "invoke failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeInvokeResponse(w, res.Status, res.Headers, res.Body, res.Truncated, res.DurationMs)
		return
	}

	if s.proxy == nil {
		http.Error(w, "invoke proxy not configured", http.StatusServiceUnavailable)
		return
	}
	pres, err := s.proxy.Invoke(r.Context(), &proxy.Request{
		Namespace:   s.functions.Namespace,
		ServiceName: app.Name,
		Method:      in.Method,
		Path:        in.Path,
		Query:       url.Values(in.Query),
		Headers:     http.Header(in.Headers),
		Body:        bytes.NewReader(body),
	})
	if err != nil {
		s.logger.Warn("invoke", "err", err, "application", app.Name)
		http.Error(w, "invoke failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeInvokeResponse(w, pres.Status, pres.Headers, pres.Body, pres.Truncated, pres.DurationMs)
}

func writeInvokeResponse(w http.ResponseWriter, status int, headers http.Header, body []byte, truncated bool, durationMs int64) {
	respBody := string(body)
	isBase64 := false
	ct := strings.ToLower(headers.Get("content-type"))
	if !utf8.Valid(body) || (ct != "" && !isTextish(ct)) {
		respBody = base64.StdEncoding.EncodeToString(body)
		isBase64 = true
	}
	writeJSON(w, http.StatusOK, invokeResponseDTO{
		Status:       status,
		Headers:      map[string][]string(headers),
		Body:         respBody,
		BodyIsBase64: isBase64,
		Truncated:    truncated,
		DurationMs:   durationMs,
	})
}

// invokeWorker relays the same request shape to the worker HTTP endpoint.
// URL: {workerURL}/apps/{appName}/{path}
func (s *Server) invokeWorker(ctx context.Context, app store.Application, in invokeRequestDTO, body []byte) (workerInvokeResult, error) {
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}
	path := in.Path
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// Worker's axum routes are /apps/{app} and /apps/{app}/{*rest} — the
	// wildcard needs at least one segment, so path "/" must hit the bare
	// /apps/{app} route (dispatch_root treats it as inner_path=/).
	target := s.worker.invokeURL + "/apps/" + app.Name
	if path != "/" {
		target += path
	}
	if q := url.Values(in.Query).Encode(); q != "" {
		target += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return workerInvokeResult{}, err
	}
	for k, vs := range in.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	start := time.Now()
	resp, err := workerHTTPClient.Do(req)
	if err != nil {
		return workerInvokeResult{}, err
	}
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(resp.Body, proxy.MaxResponseBody+1))
	if err != nil {
		return workerInvokeResult{}, err
	}
	tr := n > proxy.MaxResponseBody
	b := buf.Bytes()
	if tr {
		b = b[:proxy.MaxResponseBody]
	}
	return workerInvokeResult{
		Status:     resp.StatusCode,
		Headers:    resp.Header,
		Body:       b,
		Truncated:  tr,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

type workerInvokeResult struct {
	Status     int
	Headers    http.Header
	Body       []byte
	Truncated  bool
	DurationMs int64
}

var workerHTTPClient = &http.Client{Timeout: 30 * time.Second}

// isTextish returns true for content types that render nicely as text in the UI.
func isTextish(ct string) bool {
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	if strings.HasPrefix(ct, "application/") {
		return strings.Contains(ct, "json") ||
			strings.Contains(ct, "xml") ||
			strings.Contains(ct, "yaml") ||
			strings.Contains(ct, "javascript") ||
			strings.Contains(ct, "html")
	}
	return false
}
