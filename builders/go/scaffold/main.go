// Placeholder overwritten by the user's source at build time.
package main

import (
	"fmt"
	"net/http"

	spinhttp "github.com/fermyon/spin/sdk/go/v2/http"
)

func init() {
	spinhttp.Handle(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from the spinup scaffold")
	})
}

func main() {}
