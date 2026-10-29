package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/app"
)

const sampleCSV = "../../testdata/sample-issues.csv"

func newTestServer(t *testing.T, mod func(*Options)) *Server {
	t.Helper()
	opts := Options{UploadDir: filepath.Join(t.TempDir(), "uploads"), Version: "v-test", Binary: app.BinaryInfo{GitSHA: "abc", GoVersion: "go1.x"}}
	if mod != nil {
		mod(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(s *Server, method, target string, body []byte, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Host = "localhost:8080"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func TestSecurity(t *testing.T) {
	s := newTestServer(t, nil)

	req := httptest.NewRequest("GET", "/api/version", nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("evil host: %d", rec.Code)
	}

	rec = do(s, "GET", "/api/version", nil, nil)
	if rec.Code != 200 {
		t.Errorf("localhost: %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Errorf("CSP missing: %q", rec.Header().Get("Content-Security-Policy"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("security headers missing")
	}

	if rec := do(s, "POST", "/api/uploads", nil, nil); rec.Code != 403 {
		t.Errorf("POST without client header: %d", rec.Code)
	}
	if rec := do(s, "OPTIONS", "/api/version", nil, nil); rec.Code != 403 {
		t.Errorf("OPTIONS: %d", rec.Code)
	}

	for _, host := range []string{"127.0.0.1:1", "[::1]:80", "localhost"} {
		req := httptest.NewRequest("GET", "/api/version", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("host %s: %d", host, rec.Code)
		}
	}

	remote := newTestServer(t, func(o *Options) { o.AllowRemote = true; o.DataDir = t.TempDir() })
	req = httptest.NewRequest("GET", "/api/version", nil)
	req.Host = "forecast.internal"
	rec = httptest.NewRecorder()
	remote.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("allow-remote host: %d", rec.Code)
	}
}

func TestVersionAndDefaults(t *testing.T) {
	s := newTestServer(t, func(o *Options) { o.DefaultInput = "x.csv"; o.DefaultExclusions = "e.json" })
	var v struct {
		Version string         `json:"version"`
		Binary  map[string]any `json:"binary"`
	}
	decode(t, do(s, "GET", "/api/version", nil, nil), &v)
	if v.Version != "v-test" || v.Binary["git_sha"] != "abc" || v.Binary["go_version"] != "go1.x" {
		t.Errorf("version = %+v", v)
	}
	var d map[string]string
	decode(t, do(s, "GET", "/api/defaults", nil, nil), &d)
	if d["input"] != "x.csv" || d["exclusions"] != "e.json" {
		t.Errorf("defaults = %v", d)
	}
}

func upload(t *testing.T, s *Server, name string, content string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(content))
	mw.Close()
	return do(s, "POST", "/api/uploads", buf.Bytes(), map[string]string{"X-Forecast-Client": "web", "Content-Type": mw.FormDataContentType()})
}

func TestUploads(t *testing.T) {
	s := newTestServer(t, nil)
	content := "identifier\nA-1\n"

	rec := upload(t, s, "my issues.csv", content)
	if rec.Code != 200 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Path, Name string
		Size       int64
	}
	decode(t, rec, &res)
	got, err := os.ReadFile(res.Path)
	if err != nil || string(got) != content {
		t.Errorf("stored content = %q, %v", got, err)
	}
	sum := sha256.Sum256([]byte(content))
	if want := hex.EncodeToString(sum[:])[:12] + "-my_issues.csv"; filepath.Base(res.Path) != want {
		t.Errorf("stored name = %s, want %s", filepath.Base(res.Path), want)
	}
	if res.Name != "my issues.csv" || res.Size != int64(len(content)) {
		t.Errorf("response = %+v", res)
	}

	var res2 struct{ Path string }
	decode(t, upload(t, s, "my issues.csv", content), &res2)
	if res2.Path != res.Path {
		t.Errorf("same content gave %s and %s", res.Path, res2.Path)
	}

	if rec := upload(t, s, "evil.exe", "x"); rec.Code != 400 {
		t.Errorf("bad extension: %d", rec.Code)
	}
	for _, name := range []string{"mapping.yaml", "mapping.YML"} {
		if rec := upload(t, s, name, "version: 1\n"); rec.Code != 200 {
			t.Errorf("%s upload: %d %s", name, rec.Code, rec.Body)
		}
	}

	small := newTestServer(t, func(o *Options) { o.MaxUploadBytes = 10 })
	if rec := upload(t, small, "a.csv", strings.Repeat("x", 100)); rec.Code != 413 {
		t.Errorf("oversize: %d %s", rec.Code, rec.Body)
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"a.csv", ".hidden/b.csv", "x/y/z/deep.csv", "x/ok.csv", "notes.txt"} {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	s := newTestServer(t, func(o *Options) { o.AllowRemote = true; o.DataDir = dir })
	s.opts.UploadDir = filepath.Join(t.TempDir(), "up")
	os.MkdirAll(s.opts.UploadDir, 0o700)
	os.WriteFile(filepath.Join(s.opts.UploadDir, "u.csv"), []byte("x"), 0o644)

	var res struct {
		Files   []fileEntry `json:"files"`
		Uploads []fileEntry `json:"uploads"`
	}
	decode(t, do(s, "GET", "/api/files?kind=input", nil, nil), &res)
	var names []string
	for _, f := range res.Files {
		names = append(names, filepath.Base(f.Path))
	}
	if len(names) != 2 || !(contains(names, "a.csv") && contains(names, "ok.csv")) {
		t.Errorf("files = %v, want a.csv and ok.csv only", names)
	}
	if len(res.Uploads) != 1 {
		t.Errorf("uploads = %v", res.Uploads)
	}
	if rec := do(s, "GET", "/api/files?kind=bogus", nil, nil); rec.Code != 400 {
		t.Errorf("bad kind: %d", rec.Code)
	}
}

func TestFiles_ImportKinds(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"export.csv", "data.db", "jira.yaml", "other.yml", "notes.json"} {
		os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644)
	}
	s := newTestServer(t, func(o *Options) { o.AllowRemote = true; o.DataDir = dir })
	for kind, want := range map[string][]string{"raw": {"export.csv"}, "map": {"jira.yaml", "other.yml"}} {
		var res struct {
			Files []fileEntry `json:"files"`
		}
		rec := do(s, "GET", "/api/files?kind="+kind, nil, nil)
		if rec.Code != 200 {
			t.Fatalf("kind=%s: %d %s", kind, rec.Code, rec.Body)
		}
		decode(t, rec, &res)
		var names []string
		for _, f := range res.Files {
			names = append(names, filepath.Base(f.Path))
		}
		sort.Strings(names)
		if !reflect.DeepEqual(names, want) {
			t.Errorf("kind=%s: files = %v, want %v", kind, names, want)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestResolvePath(t *testing.T) {
	s := newTestServer(t, nil)
	_, err := s.resolvePath("input", "-")
	if fe, ok := err.(*app.FieldError); !ok || fe.Msg != "-input -: stdin is not available in the web UI; upload the file instead" {
		t.Errorf("stdin error = %v", err)
	}
	if abs, err := s.resolvePath("input", "rel.csv"); err != nil || !filepath.IsAbs(abs) {
		t.Errorf("relative: %q %v", abs, err)
	}

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.csv"), []byte("x"), 0o644)
	r := newTestServer(t, func(o *Options) { o.AllowRemote = true; o.DataDir = tmp })
	if _, err := r.resolvePath("input", "/etc/hosts"); err == nil || !strings.Contains(err.Error(), "outside -data-dir") {
		t.Errorf("/etc/hosts: %v", err)
	}
	if _, err := r.resolvePath("input", filepath.Join(tmp, "a.csv")); err != nil {
		t.Errorf("inside data dir: %v", err)
	}
	if _, err := r.resolvePath("input", filepath.Join(tmp, "..", "a.csv")); err == nil {
		t.Error(".. escape accepted")
	}
	link := filepath.Join(tmp, "link")
	if err := os.Symlink("/etc", link); err == nil {
		if _, err := r.resolvePath("input", filepath.Join(link, "hosts")); err == nil {
			t.Error("symlink escape accepted")
		}
	}
}

func TestDatasetCache(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mod time.Time) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o644)
		os.Chtimes(p, mod, mod)
		return p
	}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newDatasetCache(4)
	ctx := context.Background()

	a := write("a.csv", "identifier\nA-1\n", t0)
	d1, err := c.get(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := c.get(ctx, a)
	if d1 != d2 {
		t.Error("second get should hit the cache")
	}

	write("a.csv", "identifier\nA-1\nA-2\n", t0.Add(time.Hour))
	d3, err := c.get(ctx, a)
	if err != nil || d3 == d1 || len(d3.issues) != 2 {
		t.Errorf("changed file: same=%v issues=%d err=%v", d3 == d1, len(d3.issues), err)
	}

	var paths []string
	for _, n := range []string{"b.csv", "c.csv", "d.csv", "e.csv"} {
		paths = append(paths, write(n, "identifier\nX-1\n", t0))
	}
	// a is the least recently used of {a, b, c, d}; a fifth path evicts it.
	b1, _ := c.get(ctx, paths[0])
	c.get(ctx, paths[1])
	c.get(ctx, paths[2])
	e1, _ := c.get(ctx, paths[3])
	if len(c.entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(c.entries))
	}
	if b2, _ := c.get(ctx, paths[0]); b2 != b1 {
		t.Error("b should still be cached")
	}
	if e2, _ := c.get(ctx, paths[3]); e2 != e1 {
		t.Error("e should still be cached")
	}
	if d4, _ := c.get(ctx, a); d4 == d3 {
		t.Error("a should have been evicted and reloaded")
	}

	c.invalidate(paths[3])
	if e3, _ := c.get(ctx, paths[3]); e3 == e1 {
		t.Error("invalidate should drop the entry")
	}

	if _, err := c.get(ctx, filepath.Join(dir, "missing.csv")); err == nil {
		t.Error("missing file should error")
	}
}

func TestSummary(t *testing.T) {
	s := newTestServer(t, nil)
	rec := do(s, "GET", "/api/datasets/summary?input="+sampleCSV, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var res struct {
		Issues    int      `json:"issues"`
		Teams     []string `json:"teams"`
		Readiness []struct {
			Command string `json:"command"`
		} `json:"readiness"`
	}
	decode(t, rec, &res)
	raw, _ := os.ReadFile(sampleCSV)
	rows := strings.Count(strings.TrimSpace(string(raw)), "\n") // data rows = lines - header
	if res.Issues != rows || res.Issues != 30 {
		t.Errorf("issues = %d, rows = %d", res.Issues, rows)
	}
	if want := []string{"ENG"}; !reflect.DeepEqual(res.Teams, want) {
		t.Errorf("teams = %v, want %v", res.Teams, want)
	}
	if len(res.Readiness) != 5 {
		t.Errorf("readiness = %v", res.Readiness)
	}

	rec = do(s, "GET", "/api/datasets/summary?input=nope.csv", nil, nil)
	var e struct {
		Error struct {
			Fields []string `json:"fields"`
		} `json:"error"`
	}
	decode(t, rec, &e)
	if rec.Code != 400 || !reflect.DeepEqual(e.Error.Fields, []string{"input"}) {
		t.Errorf("missing file: %d %s", rec.Code, rec.Body)
	}
	if rec := do(s, "GET", "/api/datasets/summary?input=-", nil, nil); rec.Code != 400 {
		t.Errorf("stdin: %d", rec.Code)
	}
}

func TestPlotly_Vendored(t *testing.T) {
	data, err := staticFS.ReadFile("static/vendor/plotly-" + plotlyVersion + ".min.js")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != plotlySHA256 {
		t.Errorf("sha256 = %s, want %s", got, plotlySHA256)
	}
	s := newTestServer(t, nil)
	rec := do(s, "GET", plotlyPath, nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("GET %s: %d %q", plotlyPath, rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := do(s, "GET", "/", nil, nil); rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("index: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}
