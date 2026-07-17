// Package deploy is the single owner of "publish an application to the
// cluster". It Applies (or Deletes) the SpinApp CR plus its per-function
// Istio VirtualService in one call, so every caller — the httpapi /deploy
// handler and the builder's post-build auto-deploy — goes through the
// same code path.
//
// If you're adding a new resource that should follow the app lifecycle
// (a NetworkPolicy, a HorizontalPodAutoscaler, whatever), wire it here.
package deploy

import (
	"context"
	"log/slog"

	"github.com/emdzej/spinup/services/control-plane/internal/istio"
	"github.com/emdzej/spinup/services/control-plane/internal/spinapp"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
)

// Request is the caller's desired state.
type Request struct {
	App      store.Application
	Image    string
	Replicas int32
}

// Deployer owns the write-side lifecycle of a deployed application:
// SpinApp CR + optional Istio VirtualService.
type Deployer struct {
	logger        *slog.Logger
	spin          *spinapp.Client
	vs            *istio.Client
	pullSecrets   []string
	publicDomain  string
	publicGateway string
}

// New wires the shared clients. When publicDomain/publicGateway are empty
// (or vs is nil), the VS side is a no-op — legitimate for headless or
// bearer-only deployments where no ingress ever binds a function subdomain.
func New(logger *slog.Logger, spin *spinapp.Client, vs *istio.Client, pullSecrets []string, publicDomain, publicGateway string) *Deployer {
	return &Deployer{
		logger:        logger,
		spin:          spin,
		vs:            vs,
		pullSecrets:   pullSecrets,
		publicDomain:  publicDomain,
		publicGateway: publicGateway,
	}
}

// Deploy Applies the SpinApp and, when public ingress is configured, its
// VirtualService. VS failures are logged but not returned — the app is
// still up on its Service; the operator can retry ingress separately.
func (d *Deployer) Deploy(ctx context.Context, req Request) (*spinapp.Status, error) {
	st, err := d.spin.Apply(ctx, spinapp.Spec{
		Name:             req.App.Name,
		ApplicationID:    req.App.ID,
		TenantID:         req.App.TenantID,
		Image:            req.Image,
		Replicas:         req.Replicas,
		ImagePullSecrets: d.pullSecrets,
	})
	if err != nil {
		return nil, err
	}
	if d.vs != nil && d.publicDomain != "" && d.publicGateway != "" {
		if err := d.vs.Apply(ctx, istio.Spec{
			Name:            req.App.Name,
			ApplicationID:   req.App.ID,
			Host:            req.App.Name + "." + d.publicDomain,
			Gateway:         d.publicGateway,
			DestinationHost: req.App.Name,
			DestinationPort: 80,
		}); err != nil {
			d.logger.Warn("apply virtualservice", "err", err, "name", req.App.Name)
		}
	}
	return st, nil
}

// Undeploy removes the SpinApp and its VS. SpinApp errors bubble; VS
// errors are logged but not returned (the SpinApp is already gone —
// orphan config, not a broken deployment).
func (d *Deployer) Undeploy(ctx context.Context, appName string) error {
	if err := d.spin.Delete(ctx, appName); err != nil {
		return err
	}
	if d.vs != nil {
		if err := d.vs.Delete(ctx, appName); err != nil {
			d.logger.Warn("delete virtualservice", "err", err, "name", appName)
		}
	}
	return nil
}
