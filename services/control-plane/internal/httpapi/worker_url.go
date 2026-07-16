package httpapi

// workerRuntime bundles the two URLs the control plane needs to know about the
// worker: one it proxies invokes to (server-side), and one it shows in the UI.
type workerRuntime struct {
	invokeURL string
	uiURL     string
}

func (w workerRuntime) publicURLForUI() string {
	if w.uiURL != "" {
		return w.uiURL
	}
	return w.invokeURL
}

// workerInvokeURL returns the URL a user can hit externally (via port-forward
// or gateway) for a given app.
func workerInvokeURL(base, appName string) string {
	if base == "" {
		return ""
	}
	return base + "/apps/" + appName
}
