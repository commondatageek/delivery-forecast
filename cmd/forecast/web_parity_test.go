package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/commondatageek/delivery-forecast/internal/web"
)

// webIDs maps each CLI entry point to the web command ID that exposes it.
func webIDs() map[uintptr]string {
	return map[uintptr]string{
		reflect.ValueOf(cmdSimItems).Pointer():       "sim-items",
		reflect.ValueOf(cmdSimDays).Pointer():        "sim-days",
		reflect.ValueOf(cmdSimProbability).Pointer(): "sim-probability",
		reflect.ValueOf(cmdSimBacktest).Pointer():    "sim-backtest",
		reflect.ValueOf(cmdHistory).Pointer():        "history",
		reflect.ValueOf(cmdCFD).Pointer():            "cfd",
		reflect.ValueOf(cmdAging).Pointer():          "aging",
		reflect.ValueOf(cmdCount).Pointer():          "count",
		reflect.ValueOf(cmdCheck).Pointer():          "check",
	}
}

// splitGolden splits a golden file into its stdout, stderr and error sections.
func splitGolden(t *testing.T, s string) (stdout, stderr, errText string) {
	t.Helper()
	const o, e, r = "==== stdout\n", "==== stderr\n", "==== error\n"
	i, j, k := strings.Index(s, o), strings.Index(s, e), strings.Index(s, r)
	if i != 0 || j < 0 || k < 0 {
		t.Fatalf("malformed golden file")
	}
	return s[len(o):j], s[j+len(e) : k], strings.TrimSuffix(s[k+len(r):], "\n")
}

// formatOf names the export a CLI -format value selects, given the command's
// default.
func exportFor(id, format string) string {
	switch {
	case format == "csv" || format == "json" || format == "html":
		return format
	case format == "text":
		return "text"
	}
	switch id {
	case "cfd":
		return "html"
	case "history":
		return "csv"
	}
	return "text"
}

// TestWebParity replays every CLI golden case through the HTTP API and
// requires the same output, warnings and errors.
func TestWebParity(t *testing.T) {
	pinGoldenClock(t)
	srv, err := web.New(web.Options{UploadDir: t.TempDir(), Now: nowFunc, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	hostFix := func(req *http.Request) { req.Host = "localhost" }

	ids := webIDs()
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			id, ok := ids[reflect.ValueOf(c.run).Pointer()]
			if !ok {
				t.Fatalf("golden case %s maps to no web command", c.name)
			}
			p, err := web.ParamsFromArgs(id, c.args, nowFunc())
			if err != nil {
				t.Fatal(err)
			}
			format := p["format"]
			delete(p, "format")

			wantB, err := os.ReadFile(filepath.Join("testdata", "golden", c.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			wantOut, wantErrStream, wantErr := splitGolden(t, string(wantB))
			var wantWarn []string
			for _, line := range strings.Split(wantErrStream, "\n") {
				if strings.HasPrefix(line, "WARN  ") {
					wantWarn = append(wantWarn, strings.TrimPrefix(line, "WARN  "))
				}
			}

			body, _ := json.Marshal(map[string]any{"params": p})
			req, _ := http.NewRequest("POST", ts.URL+"/api/run/"+id, bytes.NewReader(body))
			req.Header.Set("X-Forecast-Client", "web")
			hostFix(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			var gotErr string
			var gotWarn []string
			var runID string
			if resp.StatusCode != 200 {
				var e struct {
					Error    struct{ Message string } `json:"error"`
					Warnings []string                 `json:"warnings"`
				}
				json.NewDecoder(resp.Body).Decode(&e)
				gotErr, gotWarn = e.Error.Message, e.Warnings
			} else {
				sc := bufio.NewScanner(resp.Body)
				sc.Buffer(make([]byte, 1<<20), 64<<20)
				for sc.Scan() {
					var ev struct {
						Type     string
						RunID    string `json:"run_id"`
						Warnings []string
						Error    struct{ Message string }
					}
					if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
						t.Fatal(err)
					}
					switch ev.Type {
					case "error":
						gotErr, gotWarn = ev.Error.Message, ev.Warnings
					case "result":
						runID, gotWarn = ev.RunID, ev.Warnings
					}
				}
			}

			if wantErr != "<nil>" {
				if gotErr != wantErr {
					t.Errorf("error = %q, want %q", gotErr, wantErr)
				}
				if wantOut != "" {
					t.Errorf("golden has both stdout and an error; fix the test")
				}
			} else {
				if gotErr != "" {
					t.Fatalf("unexpected error %q", gotErr)
				}
				xreq, _ := http.NewRequest("GET", ts.URL+"/api/runs/"+runID+"/export/"+exportFor(id, format), nil)
				hostFix(xreq)
				xresp, err := http.DefaultClient.Do(xreq)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := io.ReadAll(xresp.Body)
				xresp.Body.Close()
				if xresp.StatusCode != 200 {
					t.Fatalf("export: %d %s", xresp.StatusCode, got)
				}
				if string(got) != wantOut {
					t.Errorf("output differs: %s", firstDiff(wantOut, string(got)))
				}
			}
			if !reflect.DeepEqual(append([]string{}, gotWarn...), append([]string{}, wantWarn...)) {
				t.Errorf("warnings = %q, want %q", gotWarn, wantWarn)
			}
		})
	}
}

// TestWebSummariesMatchCLI keeps the web command registry's summaries
// identical to the CLI's command tables.
func TestWebSummariesMatchCLI(t *testing.T) {
	cli := map[string]string{}
	for _, c := range topCommands {
		cli[c.Name] = c.Summary
	}
	for _, c := range simCommands {
		cli["sim "+c.Name] = c.Summary
	}
	for name, summary := range web.CommandSummaries() {
		if cli[name] != summary {
			t.Errorf("%s: web summary %q, CLI summary %q", name, summary, cli[name])
		}
	}
}
