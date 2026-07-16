// Package promql is a minimal client for the Prometheus HTTP API's
// query_range endpoint. Works with VictoriaMetrics too (same wire format).
package promql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// Point is a (timestamp seconds, float value) tuple.
type Point struct {
	T float64
	V float64
}

// MarshalJSON emits [ts, val] to match uPlot / Chart.js shapes.
func (p Point) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("[%v,%v]", p.T, p.V)), nil
}

// QueryRange runs an instant PromQL query over [start,end] with the given step.
// Returns the first result series' points (callers should shape queries so they
// aggregate to a single series). Missing / empty results return nil, nil.
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Point, error) {
	if c.base == "" {
		return nil, fmt.Errorf("prometheus URL not configured")
	}
	v := url.Values{}
	v.Set("query", query)
	v.Set("start", strconv.FormatFloat(float64(start.Unix()), 'f', -1, 64))
	v.Set("end", strconv.FormatFloat(float64(end.Unix()), 'f', -1, 64))
	v.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))

	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/api/v1/query_range?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("prom %d for query %q", res.StatusCode, query)
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode prom response: %w", err)
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prom status: %s", body.Status)
	}
	if len(body.Data.Result) == 0 {
		return nil, nil
	}
	// Aggregated queries produce a single result. If callers use `sum(...)`
	// or similar the response is one series regardless of underlying pods.
	raw := body.Data.Result[0].Values
	out := make([]Point, 0, len(raw))
	for _, pt := range raw {
		if len(pt) != 2 {
			continue
		}
		ts, _ := pt[0].(float64)
		vs, _ := pt[1].(string)
		v, err := strconv.ParseFloat(vs, 64)
		if err != nil {
			continue
		}
		out = append(out, Point{T: ts, V: v})
	}
	return out, nil
}
