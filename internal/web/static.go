package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFS embed.FS

// plotlyVersion and plotlySHA256 pin the vendored Plotly bundle (D14).
const (
	plotlyVersion = "2.35.2"
	plotlySHA256  = "6d21266ce1bd7d9e5ab4e115989c70c20de0382fd973a8f26ab58619eba4d603"
)

// plotlyPath is where the UI loads Plotly from.
const plotlyPath = "/vendor/plotly-" + plotlyVersion + ".min.js"

// staticHandler serves the embedded UI. Everything is no-cache except the
// versioned vendor files, which are immutable.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vendor/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
