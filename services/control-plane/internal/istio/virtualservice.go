// Package istio is a minimal dynamic-client wrapper for the Istio
// networking.istio.io/v1alpha3 VirtualService resource.
//
// Kept as a thin unstructured layer so we don't have to import the full Istio
// Go module (heavy transitive deps). We only need Apply/Delete for the
// public-function routing feature.
package istio

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var GVR = schema.GroupVersionResource{
	Group:    "networking.istio.io",
	Version:  "v1alpha3",
	Resource: "virtualservices",
}

const (
	kind       = "VirtualService"
	apiVersion = "networking.istio.io/v1alpha3"

	fieldManager = "spinup-control-plane"
)

// Standard CP-emitted resource markers. Kept in one place so every writer
// tags resources consistently.
const (
	LabelManagedBy      = "app.kubernetes.io/managed-by"
	LabelManagedByVal   = "spinup"
	LabelApplication    = "spinup.emdzej.pl/application"
	LabelApplicationID  = "spinup.emdzej.pl/application-id"
	AnnotationEmittedBy = "spinup.emdzej.pl/emitted-by"
	AnnotationEmittedByVal = "control-plane"
)

// Spec describes the VirtualService we want to bind for a function.
//
// A VS lives in the functions namespace alongside the SpinApp. It references
// the platform Gateway (in a different namespace) via `<gwNs>/<gwName>`.
type Spec struct {
	// Name of the VS resource (matches the SpinApp/Service name).
	Name string
	// ApplicationID is stamped as a label so operators can trace CRs back
	// to the SpinUP application row.
	ApplicationID string
	// Host is the public FQDN, e.g. "hello.spinup.solvely.pl".
	Host string
	// Gateway is the "<namespace>/<name>" reference to the platform Gateway.
	Gateway string
	// DestinationHost is the in-namespace Service name to route to. SpinKube
	// creates a Service with the same name as the SpinApp.
	DestinationHost string
	// DestinationPort is the Service port (typically 80).
	DestinationPort int32
}

type Client struct {
	res dynamic.NamespaceableResourceInterface
	ns  string
}

func New(dyn dynamic.Interface, namespace string) *Client {
	return &Client{res: dyn.Resource(GVR), ns: namespace}
}

// Apply creates or updates the VirtualService using server-side apply.
// Idempotent — safe to call on every Deploy.
func (c *Client) Apply(ctx context.Context, s Spec) error {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      s.Name,
			"namespace": c.ns,
			"labels": map[string]any{
				LabelManagedBy:     LabelManagedByVal,
				LabelApplication:   s.Name,
				LabelApplicationID: s.ApplicationID,
			},
			"annotations": map[string]any{
				AnnotationEmittedBy: AnnotationEmittedByVal,
			},
		},
		"spec": map[string]any{
			"hosts":    []any{s.Host},
			"gateways": []any{s.Gateway},
			"http": []any{
				map[string]any{
					"route": []any{
						map[string]any{
							"destination": map[string]any{
								"host": s.DestinationHost,
								"port": map[string]any{
									"number": int64(s.DestinationPort),
								},
							},
						},
					},
				},
			},
		},
	}}
	data, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal virtualservice: %w", err)
	}
	_, err = c.res.Namespace(c.ns).Patch(ctx, s.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        ptrBool(true),
	})
	if err != nil {
		return fmt.Errorf("apply virtualservice: %w", err)
	}
	return nil
}

// Delete removes the VirtualService. NotFound is treated as success.
func (c *Client) Delete(ctx context.Context, name string) error {
	err := c.res.Namespace(c.ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func ptrBool(b bool) *bool { return &b }
