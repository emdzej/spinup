// Package builder orchestrates source -> WASM -> OCI-push builds as K8s Jobs.
// It's intentionally minimal: one Job per build, source shipped via a Secret,
// image ref generated as {registryURL}/{functionName}:{buildID}.
package builder

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/emdzej/spinup/services/control-plane/internal/istio"
	"github.com/emdzej/spinup/services/control-plane/internal/spinapp"
	"github.com/emdzej/spinup/services/control-plane/internal/store"
	"github.com/emdzej/spinup/services/control-plane/internal/telemetry"
)

const (
	labelManagedBy     = "app.kubernetes.io/managed-by"
	labelManagedByVal  = "spinup"
	labelBuildID       = "spinup.io/build-id"
	labelApplicationID = "spinup.io/application-id"
	labelAppName       = "spinup.io/application"
)

type Config struct {
	Logger      *slog.Logger
	Kube        kubernetes.Interface
	Store       store.Store
	Spin        *spinapp.Client
	Namespace   string
	GoImage     string
	JSImage     string
	TSImage     string
	RustImage   string
	RegistryURL string
	// AuthSecret names a kubernetes.io/dockerconfigjson Secret to mount at
	// /root/.docker/config.json in the builder pod. Empty = anonymous.
	AuthSecret string
	// ImagePullSecrets are attached to every build Pod so the kubelet can
	// pull the builder image from a private registry. The Secrets must live
	// in Namespace (same as the build Jobs). Empty = public registry.
	ImagePullSecrets []string
	// FunctionsImagePullSecrets are stamped onto every SpinApp CR the
	// builder auto-applies after a successful build. Kubelet uses these
	// to pull the freshly-pushed function image.
	FunctionsImagePullSecrets []string
	// VS + PublicDomain + PublicGateway mirror what the httpapi deploy
	// handler carries. When all three are set, the auto-deploy path also
	// emits a VirtualService for <app>.<publicDomain> alongside the SpinApp.
	VS            *istio.Client
	PublicDomain  string
	PublicGateway string
	Metrics       *telemetry.Metrics
}

// languageProfile pins per-language build wiring: which builder image runs the Job.
// The user's source files are packed into a tar.gz and mounted at
// /source/source.tar.gz; the entrypoint overlays them onto the scaffold.
type languageProfile struct {
	image string
}

func (r *Runner) profile(lang string) (languageProfile, error) {
	switch lang {
	case "go":
		return languageProfile{image: r.cfg.GoImage}, nil
	case "js":
		return languageProfile{image: r.cfg.JSImage}, nil
	case "ts":
		return languageProfile{image: r.cfg.TSImage}, nil
	case "rust":
		return languageProfile{image: r.cfg.RustImage}, nil
	default:
		return languageProfile{}, fmt.Errorf("language %q has no builder", lang)
	}
}

type Runner struct {
	cfg Config
}

func New(cfg Config) *Runner { return &Runner{cfg: cfg} }

// FunctionBuildInput bundles one function's Store row with its source tree.
type FunctionBuildInput struct {
	Function store.Function
	Source   store.Source
}

// Start builds an Application by packing all its functions into one OCI image.
// The tarball layout sent to the builder Job is:
//
//	/spin.toml                     (synthesized from language + functions)
//	/functions/{name}/{user files} (one subdir per function)
//
// The builder image's entrypoint overlays the language scaffold into each
// functions/{name}/ subdir (user files win on conflict), then runs
// `spin build` + `spin registry push`.
func (r *Runner) Start(ctx context.Context, app store.Application, fns []FunctionBuildInput) (store.Build, error) {
	prof, err := r.profile(app.Language)
	if err != nil {
		return store.Build{}, err
	}
	if len(fns) == 0 {
		return store.Build{}, fmt.Errorf("application has no functions to build")
	}
	buildID := newID()
	imageRef := fmt.Sprintf("%s/%s:%s", r.cfg.RegistryURL, app.Name, buildID)

	// Build the tar tree in memory: spin.toml + per-function user files.
	specs := make([]FunctionSpec, 0, len(fns))
	files := map[string]string{}
	for _, fb := range fns {
		specs = append(specs, FunctionSpec{Name: fb.Function.Name, Route: fb.Function.Route})
		for relPath, content := range fb.Source.Files {
			files["functions/"+fb.Function.Name+"/"+relPath] = content
		}
	}
	manifest, err := synthesizeSpinToml(app.Language, app.Name, specs)
	if err != nil {
		return store.Build{}, err
	}
	files["spin.toml"] = manifest

	build := store.Build{
		ID:            buildID,
		ApplicationID: app.ID,
		ImageRef:      imageRef,
		Status:        store.BuildPending,
		CreatedAt:     time.Now().UTC(),
	}
	if err := r.cfg.Store.CreateBuild(ctx, build); err != nil {
		return store.Build{}, fmt.Errorf("create build row: %w", err)
	}

	if err := r.createSecret(ctx, buildID, files); err != nil {
		r.fail(context.Background(), buildID, "create source secret: "+err.Error())
		return build, err
	}
	if err := r.createJob(ctx, buildID, app, imageRef, prof.image); err != nil {
		r.fail(context.Background(), buildID, "create job: "+err.Error())
		return build, err
	}

	if r.cfg.Metrics != nil {
		r.cfg.Metrics.BuildsStarted.Add(ctx, 1)
	}

	// Detach from the request context so the watcher outlives it.
	go r.watch(context.Background(), buildID, app, imageRef, build.CreatedAt)
	return build, nil
}

// PodLogsByLabel streams the logs of the first pod matching the label
// selector in the functions namespace. Returns nil if no matching pod.
// Follow keeps the connection open until the container exits; tailLines
// limits the historical replay before the follow starts.
func (r *Runner) PodLogsByLabel(ctx context.Context, labelSelector string, follow bool, tailLines int64) (io.ReadCloser, error) {
	pods, err := r.cfg.Kube.CoreV1().Pods(r.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, nil
	}
	opts := &corev1.PodLogOptions{Follow: follow}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}
	return r.cfg.Kube.CoreV1().Pods(r.cfg.Namespace).GetLogs(pods.Items[0].Name, opts).Stream(ctx)
}

// Logs streams the builder pod's logs. Returns nil if no pod exists yet.
// When follow is true, the stream stays open until the container exits.
func (r *Runner) Logs(ctx context.Context, buildID string, follow bool) (io.ReadCloser, error) {
	pods, err := r.cfg.Kube.CoreV1().Pods(r.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelBuildID + "=" + buildID,
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, nil
	}
	req := r.cfg.Kube.CoreV1().Pods(r.cfg.Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{
		Follow: follow,
	})
	return req.Stream(ctx)
}

func (r *Runner) createSecret(ctx context.Context, buildID string, files map[string]string) error {
	tarball, err := packFiles(files)
	if err != nil {
		return fmt.Errorf("pack source: %w", err)
	}
	_, err = r.cfg.Kube.CoreV1().Secrets(r.cfg.Namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName(buildID),
			Namespace: r.cfg.Namespace,
			Labels: map[string]string{
				labelManagedBy: labelManagedByVal,
				labelBuildID:   buildID,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"source.tar.gz": tarball},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// packFiles builds a gzipped tar of the source tree with deterministic ordering.
// Paths are cleaned (no ".." escapes), directories are created implicitly by
// the resulting tar's file entries.
func packFiles(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		clean := path.Clean("/" + name)
		if clean == "/" || strings.HasPrefix(clean, "/..") {
			return nil, fmt.Errorf("invalid source path %q", name)
		}
		content := files[name]
		hdr := &tar.Header{
			Name:    strings.TrimPrefix(clean, "/"),
			Mode:    0o644,
			Size:    int64(len(content)),
			ModTime: time.Unix(0, 0),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *Runner) createJob(ctx context.Context, buildID string, app store.Application, imageRef, builderImage string) error {
	ttl := int32(600) // 10 min: give the log fetcher time to grab output before GC
	backoff := int32(0)

	volumes := []corev1.Volume{{
		Name: "source",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: secretName(buildID)},
		},
	}}
	mounts := []corev1.VolumeMount{{
		Name:      "source",
		MountPath: "/source",
	}}
	if r.cfg.AuthSecret != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "registry-auth",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: r.cfg.AuthSecret,
					Items: []corev1.KeyToPath{
						{Key: ".dockerconfigjson", Path: "config.json"},
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "registry-auth",
			MountPath: "/root/.docker",
			ReadOnly:  true,
		})
	}

	_, err := r.cfg.Kube.BatchV1().Jobs(r.cfg.Namespace).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName(buildID),
			Namespace: r.cfg.Namespace,
			Labels: map[string]string{
				labelManagedBy:    labelManagedByVal,
				labelBuildID:      buildID,
				labelApplicationID: app.ID,
				labelAppName:       app.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:     labelManagedByVal,
						labelBuildID:       buildID,
						labelApplicationID: app.ID,
						labelAppName:       app.Name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: pullSecretRefs(r.cfg.ImagePullSecrets),
					Containers: []corev1.Container{{
						Name:            "builder",
						Image:           builderImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Env: []corev1.EnvVar{
							{Name: "IMAGE_REF", Value: imageRef},
							{Name: "APPLICATION_NAME", Value: app.Name},
						},
						VolumeMounts: mounts,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					}},
					Volumes: volumes,
				},
			},
		},
	}, metav1.CreateOptions{})
	return err
}

func (r *Runner) watch(ctx context.Context, buildID string, app store.Application, imageRef string, startedAt time.Time) {
	logger := r.cfg.Logger.With("build_id", buildID, "application", app.Name)
	logger.Info("build watch started")

	defer func() {
		if r.cfg.Metrics != nil {
			r.cfg.Metrics.BuildDuration.Record(ctx, time.Since(startedAt).Seconds())
		}
	}()

	// Mark running once the pod starts (best effort).
	_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildRunning, "", nil)

	var finalJob *batchv1.Job
	err := wait.PollUntilContextTimeout(ctx, 3*time.Second, 15*time.Minute, true, func(ctx context.Context) (bool, error) {
		job, err := r.cfg.Kube.BatchV1().Jobs(r.cfg.Namespace).Get(ctx, jobName(buildID), metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if job.Status.Succeeded > 0 {
			finalJob = job
			return true, nil
		}
		if job.Status.Failed > 0 {
			finalJob = job
			return true, nil
		}
		return false, nil
	})

	now := time.Now().UTC()
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "build timed out after 15m"
		}
		logger.Error("build watch failed", "err", msg)
		_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildFailed, msg, &now)
		r.recordFinish(ctx, "failed")
		return
	}

	if finalJob.Status.Failed > 0 {
		msg := "job failed"
		for _, c := range finalJob.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Message != "" {
				msg = c.Message
				break
			}
		}
		logger.Info("build failed", "reason", msg)
		_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildFailed, msg, &now)
		r.recordFinish(ctx, "failed")
		return
	}

	// Extract the OCI image size from the builder pod's log.
	// The entrypoint prints a line like `SPINUP_IMAGE_SIZE_BYTES=1234567`
	// after `spin registry push`. Best-effort — a missing line is not fatal.
	if size, ok := r.extractImageSize(ctx, buildID); ok {
		if err := r.cfg.Store.UpdateBuildImageSize(ctx, buildID, size); err != nil {
			logger.Warn("persist image size", "err", err)
		}
	}

	// For workerpool apps: no SpinApp CR. The image is published to the OCI
	// registry — the worker pulls it on demand via /worker-config polling.
	if app.Runtime == store.RuntimeWorkerPool {
		logger.Info("build succeeded (workerpool — no SpinApp)", "image", imageRef)
		_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildSucceeded, "", &now)
		if r.cfg.Metrics != nil {
			r.cfg.Metrics.DeploysApplied.Add(ctx, 1)
		}
		r.recordFinish(ctx, "succeeded")
		return
	}

	logger.Info("build succeeded, applying SpinApp", "image", imageRef)
	if _, err := r.cfg.Spin.Apply(ctx, spinapp.Spec{
		Name:             app.Name,
		ApplicationID:    app.ID,
		TenantID:         app.TenantID,
		Image:            imageRef,
		Replicas:         1,
		ImagePullSecrets: r.cfg.FunctionsImagePullSecrets,
	}); err != nil {
		logger.Error("apply spinapp after build", "err", err)
		_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildFailed, "apply spinapp: "+err.Error(), &now)
		r.recordFinish(ctx, "failed")
		return
	}
	// Emit the public VirtualService alongside the SpinApp, matching what
	// the httpapi /deploy handler does. Non-fatal — build/deploy already
	// succeeded; missing VS just means no public route yet.
	if r.cfg.VS != nil && r.cfg.PublicDomain != "" && r.cfg.PublicGateway != "" {
		if err := r.cfg.VS.Apply(ctx, istio.Spec{
			Name:            app.Name,
			ApplicationID:   app.ID,
			Host:            app.Name + "." + r.cfg.PublicDomain,
			Gateway:         r.cfg.PublicGateway,
			DestinationHost: app.Name,
			DestinationPort: 80,
		}); err != nil {
			logger.Warn("apply virtualservice after build", "err", err)
		}
	}
	_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildSucceeded, "", &now)
	if r.cfg.Metrics != nil {
		r.cfg.Metrics.DeploysApplied.Add(ctx, 1)
	}
	r.recordFinish(ctx, "succeeded")
}

func (r *Runner) recordFinish(ctx context.Context, outcome string) {
	if r.cfg.Metrics == nil {
		return
	}
	r.cfg.Metrics.BuildsFinished.Add(ctx, 1,
		metric.WithAttributes(attribute.String("outcome", outcome)))
}

func (r *Runner) fail(ctx context.Context, buildID, msg string) {
	now := time.Now().UTC()
	_ = r.cfg.Store.UpdateBuildStatus(ctx, buildID, store.BuildFailed, msg, &now)
}

// extractImageSize reads the completed build pod's log and looks for the
// `SPINUP_IMAGE_SIZE_BYTES=<int>` marker emitted by the builder entrypoint
// after `spin registry push`.
func (r *Runner) extractImageSize(ctx context.Context, buildID string) (int64, bool) {
	stream, err := r.Logs(ctx, buildID, false)
	if err != nil || stream == nil {
		return 0, false
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "SPINUP_IMAGE_SIZE_BYTES=") {
			continue
		}
		v := strings.TrimPrefix(line, "SPINUP_IMAGE_SIZE_BYTES=")
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

func secretName(buildID string) string { return "src-" + buildID }
func jobName(buildID string) string    { return "build-" + buildID }

// newID returns a UUID with dashes stripped — shorter than the full UUID
// string form, still 128 bits of entropy, and DNS-1123-safe for use in
// Secret and Job names.
func newID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// pullSecretRefs turns a list of Secret names into the LocalObjectReference
// slice the PodSpec expects. Returns nil (not empty slice) when the input is
// empty so unmarshaled specs stay clean in dry-runs.
func pullSecretRefs(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	out := make([]corev1.LocalObjectReference, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, corev1.LocalObjectReference{Name: n})
		}
	}
	return out
}
