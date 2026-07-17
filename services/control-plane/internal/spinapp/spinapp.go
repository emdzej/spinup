// Package spinapp is a thin client for the SpinApp CRD.
// Writes go through the dynamic client with unstructured objects so we don't
// have to import (and version-pin) the SpinKube Go module.
package spinapp

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// GVR of core.spinkube.dev/v1alpha1 SpinApp.
var GVR = schema.GroupVersionResource{
	Group:    "core.spinkube.dev",
	Version:  "v1alpha1",
	Resource: "spinapps",
}

const (
	kind       = "SpinApp"
	apiVersion = "core.spinkube.dev/v1alpha1"

	// Executor is the SpinAppExecutor referenced by every SpinApp we create.
	// SpinKube installs one named "containerd-shim-spin" by default.
	defaultExecutor    = "containerd-shim-spin"
	labelManagedBy     = "app.kubernetes.io/managed-by"
	labelManagedByVal  = "spinup"
	labelAppName       = "spinup.io/application"
	labelApplicationID = "spinup.io/application-id"
	fieldManager       = "spinup-control-plane"
	annotationTenantID = "spinup.io/tenant"
	// Standard CP marker so operators can grep for every resource the
	// control-plane emits: `kubectl get all -A -o yaml | grep spinup.io/emitted-by`.
	annotationEmittedBy    = "spinup.io/emitted-by"
	annotationEmittedByVal = "control-plane"
)

// Spec is the caller-provided desired state.
type Spec struct {
	// Name is used as the SpinApp resource name; must be DNS-1123.
	Name          string
	ApplicationID string
	TenantID      string
	Image         string
	Replicas      int32
	Executor      string // optional; defaults to "containerd-shim-spin"
	// ImagePullSecrets are set on the resulting Pod spec so kubelet can pull
	// the image from a private registry. Secrets must live in the same
	// namespace as the SpinApp.
	ImagePullSecrets []string
}

// Status is the caller-visible read model.
type Status struct {
	Name             string
	Namespace        string
	Image            string
	Replicas         int32
	Ready            bool
	ObservedReplicas int32
	Message          string
}

// Client wraps a namespaced dynamic client for the SpinApp resource.
type Client struct {
	res dynamic.NamespaceableResourceInterface
	ns  string
}

func New(dyn dynamic.Interface, namespace string) *Client {
	return &Client{res: dyn.Resource(GVR), ns: namespace}
}

// Apply creates or updates the SpinApp using server-side apply. Idempotent.
func (c *Client) Apply(ctx context.Context, s Spec) (*Status, error) {
	if s.Executor == "" {
		s.Executor = defaultExecutor
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      s.Name,
			"namespace": c.ns,
			"labels": map[string]any{
				labelManagedBy:     labelManagedByVal,
				labelAppName:       s.Name,
				labelApplicationID: s.ApplicationID,
			},
			"annotations": map[string]any{
				annotationTenantID: s.TenantID,
				annotationEmittedBy: annotationEmittedByVal,
			},
		},
		"spec": func() map[string]any {
			spec := map[string]any{
				"image":    s.Image,
				"executor": s.Executor,
				"replicas": s.Replicas,
			}
			if len(s.ImagePullSecrets) > 0 {
				refs := make([]any, 0, len(s.ImagePullSecrets))
				for _, n := range s.ImagePullSecrets {
					refs = append(refs, map[string]any{"name": n})
				}
				spec["imagePullSecrets"] = refs
			}
			return spec
		}(),
	}}
	data, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal spinapp: %w", err)
	}
	applied, err := c.res.Namespace(c.ns).Patch(ctx, s.Name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        ptrBool(true),
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("SpinApp CRD not installed on this cluster (install SpinKube first): %w", err)
		}
		return nil, fmt.Errorf("apply spinapp: %w", err)
	}
	return statusFrom(applied), nil
}

// Get returns the current SpinApp status, or (nil, nil) if not found.
func (c *Client) Get(ctx context.Context, name string) (*Status, error) {
	obj, err := c.res.Namespace(c.ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return statusFrom(obj), nil
}

// Delete removes the SpinApp. NotFound is treated as success.
func (c *Client) Delete(ctx context.Context, name string) error {
	err := c.res.Namespace(c.ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// ErrNoCRD is returned when the SpinApp CRD isn't installed on the cluster.
var ErrNoCRD = errors.New("SpinApp CRD not installed")

func statusFrom(u *unstructured.Unstructured) *Status {
	s := &Status{
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
	}
	if img, ok, _ := unstructured.NestedString(u.Object, "spec", "image"); ok {
		s.Image = img
	}
	if r, ok, _ := unstructured.NestedInt64(u.Object, "spec", "replicas"); ok {
		s.Replicas = int32(r)
	}
	if r, ok, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas"); ok {
		s.ObservedReplicas = int32(r)
		s.Ready = s.ObservedReplicas >= s.Replicas && s.Replicas > 0
	}
	if conds, ok, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); ok {
		for _, c := range conds {
			cm, _ := c.(map[string]any)
			if t, _ := cm["type"].(string); t == "Ready" {
				if m, _ := cm["message"].(string); m != "" {
					s.Message = m
				}
			}
		}
	}
	return s
}

func ptrBool(b bool) *bool { return &b }
