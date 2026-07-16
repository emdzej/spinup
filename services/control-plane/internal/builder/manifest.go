package builder

import (
	"fmt"
	"sort"
	"strings"
)

// FunctionSpec is the input for synthesizing a component + trigger block in
// spin.toml. Callers derive it from store.Function.
type FunctionSpec struct {
	Name  string // component name; sanitized to spin-valid ident by caller
	Route string // spin trigger route, e.g. "/..." or "/hello/..."
}

// synthesizeSpinToml renders a Spin v2 manifest with N HTTP triggers and N
// components, one per function. Each component builds inside functions/{name}/
// via the language-specific command. The resulting WASMs live at paths the
// caller lays out on disk.
func synthesizeSpinToml(language, appName string, functions []FunctionSpec) (string, error) {
	if len(functions) == 0 {
		return "", fmt.Errorf("no functions")
	}
	// Deterministic order — matters for reproducible builds and diffs.
	sort.Slice(functions, func(i, j int) bool { return functions[i].Name < functions[j].Name })

	var b strings.Builder
	fmt.Fprintln(&b, "spin_manifest_version = 2")
	fmt.Fprintln(&b, "")
	fmt.Fprintln(&b, "[application]")
	fmt.Fprintf(&b, "name = %q\n", appName)
	fmt.Fprintln(&b, `version = "0.1.0"`)
	fmt.Fprintln(&b, "")

	for _, f := range functions {
		fmt.Fprintln(&b, "[[trigger.http]]")
		fmt.Fprintf(&b, "route = %q\n", f.Route)
		fmt.Fprintf(&b, "component = %q\n", f.Name)
		fmt.Fprintln(&b, "")
	}

	for _, f := range functions {
		writeComponent(&b, language, f.Name)
	}
	return b.String(), nil
}

func writeComponent(b *strings.Builder, language, name string) {
	workdir := "functions/" + name
	fmt.Fprintf(b, "[component.%s]\n", name)

	switch language {
	case "go":
		fmt.Fprintf(b, "source = %q\n", workdir+"/main.wasm")
		fmt.Fprintln(b, `allowed_outbound_hosts = []`)
		fmt.Fprintf(b, "[component.%s.build]\n", name)
		fmt.Fprintln(b, `command = "go tool componentize-go build"`)
		fmt.Fprintf(b, "workdir = %q\n", workdir)
		fmt.Fprintln(b, `watch = ["**/*.go", "go.mod"]`)

	case "js", "ts":
		// Spin's http-js/http-ts template outputs dist/scaffold.wasm.
		// Array command runs `npm install` (deps warmed via the image's npm
		// cache) then `npm run build` per component.
		fmt.Fprintf(b, "source = %q\n", workdir+"/dist/scaffold.wasm")
		fmt.Fprintln(b, `exclude_files = ["**/node_modules"]`)
		fmt.Fprintf(b, "[component.%s.build]\n", name)
		fmt.Fprintln(b, `command = ["npm install", "npm run build"]`)
		fmt.Fprintf(b, "workdir = %q\n", workdir)
		if language == "ts" {
			fmt.Fprintln(b, `watch = ["src/**/*.ts", "package.json"]`)
		} else {
			fmt.Fprintln(b, `watch = ["src/**/*.js", "package.json"]`)
		}

	case "rust":
		// Spin SDK 6 targets wasm32-wasip2. The wasm is named after the crate
		// (Cargo.toml [package].name) — the scaffold ships with name="scaffold".
		fmt.Fprintf(b, "source = %q\n", workdir+"/target/wasm32-wasip2/release/scaffold.wasm")
		fmt.Fprintln(b, `allowed_outbound_hosts = []`)
		fmt.Fprintf(b, "[component.%s.build]\n", name)
		fmt.Fprintln(b, `command = "cargo build --target wasm32-wasip2 --release"`)
		fmt.Fprintf(b, "workdir = %q\n", workdir)
		fmt.Fprintln(b, `watch = ["src/**/*.rs", "Cargo.toml"]`)

	default:
		// Should never happen — validated upstream.
		fmt.Fprintf(b, "# unknown language %q\n", language)
	}
	fmt.Fprintln(b, "")
}
