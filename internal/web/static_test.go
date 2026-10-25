package web

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// TestStatic_ImportsResolve checks that every file the UI references exists
// in the embedded tree and that the UI stays offline-safe and CSP-clean.
func TestStatic_ImportsResolve(t *testing.T) {
	jsImport := regexp.MustCompile(`from\s+["'](\.[^"']+)["']|import\(["'](\.[^"']+)["']\)`)
	htmlRef := regexp.MustCompile(`(?:src|href)="(/[^"]*)"`)

	err := fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		text := string(data)
		isVendor := strings.HasPrefix(p, "static/vendor/")
		ext := path.Ext(p)

		if !isVendor && (strings.Contains(text, "http://") || strings.Contains(text, "https://")) {
			t.Errorf("%s: references an external URL; everything must load from the server (D14)", p)
		}
		exists := func(ref string) {
			if _, err := fs.Stat(staticFS, ref); err != nil {
				t.Errorf("%s: references missing file %s", p, ref)
			}
		}
		switch ext {
		case ".js":
			if isVendor {
				return nil
			}
			for _, m := range jsImport.FindAllStringSubmatch(text, -1) {
				ref := m[1] + m[2]
				exists(path.Join(path.Dir(p), ref))
			}
			fallthrough
		case ".html":
			if strings.Contains(text, "innerHTML =") || strings.Contains(text, "innerHTML=") || strings.Contains(text, "onclick=") {
				t.Errorf("%s: uses innerHTML assignment or an inline handler", p)
			}
			if ext == ".html" {
				for _, m := range htmlRef.FindAllStringSubmatch(text, -1) {
					ref := strings.TrimPrefix(strings.SplitN(m[1], "?", 2)[0], "/")
					if strings.HasPrefix(m[1], "#") || ref == "" || strings.HasPrefix(m[1], "/api/") {
						continue
					}
					exists(path.Join("static", ref))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
