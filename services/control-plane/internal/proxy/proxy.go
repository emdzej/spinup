// Package proxy forwards HTTP requests to in-cluster Services via the
// Kubernetes API server's service proxy subresource
// (/api/v1/namespaces/{ns}/services/{scheme}:{name}:{port}/proxy/{path}).
//
// Works from anywhere with a rest.Config: locally (via kubeconfig) or
// in-cluster (via ServiceAccount).
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

const (
	// MaxRequestBody caps the request body sent to a function.
	MaxRequestBody = 1 << 20 // 1 MiB
	// MaxResponseBody caps the response body returned to the caller.
	MaxResponseBody = 1 << 20 // 1 MiB
)

type Client struct {
	http    *http.Client
	baseURL string
}

func New(cfg *rest.Config) (*Client, error) {
	tCfg, err := cfg.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("transport config: %w", err)
	}
	rt, err := transport.New(tCfg)
	if err != nil {
		return nil, fmt.Errorf("build transport: %w", err)
	}
	return &Client{
		http:    &http.Client{Transport: rt, Timeout: 30 * time.Second},
		baseURL: strings.TrimRight(cfg.Host, "/"),
	}, nil
}

// Request describes an invocation to relay to a Service.
type Request struct {
	Namespace   string
	ServiceName string
	// Method: GET/POST/PUT/DELETE/PATCH. Empty defaults to GET.
	Method string
	// Path: /hello — leading slash optional. Empty means "/".
	Path string
	// Query and Headers may be nil.
	Query   url.Values
	Headers http.Header
	// Body is copied verbatim; caller enforces size limits.
	Body io.Reader
}

// Response is what the caller sees.
type Response struct {
	Status     int
	Headers    http.Header
	Body       []byte
	Truncated  bool
	DurationMs int64
}

func (c *Client) Invoke(ctx context.Context, in *Request) (*Response, error) {
	if in.ServiceName == "" || in.Namespace == "" {
		return nil, fmt.Errorf("namespace and serviceName are required")
	}
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

	// Use port 80 explicitly to match SpinKube's Service port.
	proxyURL := fmt.Sprintf("%s/api/v1/namespaces/%s/services/http:%s:80/proxy%s",
		c.baseURL, in.Namespace, in.ServiceName, path)
	if q := in.Query.Encode(); q != "" {
		proxyURL += "?" + q
	}

	req, err := http.NewRequestWithContext(ctx, method, proxyURL, in.Body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, vs := range in.Headers {
		// Skip hop-by-hop and auth headers — the K8s API server injects its own.
		switch strings.ToLower(k) {
		case "host", "connection", "authorization", "content-length":
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy call: %w", err)
	}
	defer resp.Body.Close()

	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(resp.Body, MaxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	truncated := n > MaxResponseBody
	body := buf.Bytes()
	if truncated {
		body = body[:MaxResponseBody]
	}

	return &Response{
		Status:     resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
		Truncated:  truncated,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
