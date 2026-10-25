package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type event struct {
	Type     string          `json:"type"`
	Done     int             `json:"done"`
	Total    int             `json:"total"`
	RunID    string          `json:"run_id"`
	Command  string          `json:"command"`
	Text     string          `json:"text"`
	CLI      string          `json:"cli"`
	Exports  []string        `json:"exports"`
	Warnings []string        `json:"warnings"`
	Result   json.RawMessage `json:"result"`
	Error    struct {
		Message string   `json:"message"`
		Fields  []string `json:"fields"`
	} `json:"error"`
}

// post runs a command and returns the HTTP status, the events (for a 200) and
// the decoded error body (for a non-200).
func post(t *testing.T, s *Server, id string, params map[string]string) (int, []event) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"params": params})
	rec := do(s, "POST", "/api/run/"+id, body, map[string]string{"X-Forecast-Client": "web"})
	var evs []event
	if rec.Code != 200 {
		var e struct {
			Error struct {
				Message string   `json:"message"`
				Fields  []string `json:"fields"`
			} `json:"error"`
		}
		decode(t, rec, &e)
		return rec.Code, []event{{Type: "error", Error: e.Error}}
	}
	sc := bufio.NewScanner(rec.Body)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad event %q: %v", sc.Text(), err)
		}
		evs = append(evs, ev)
	}
	return 200, evs
}

func fixedServer(t *testing.T) *Server {
	return newTestServer(t, func(o *Options) {
		o.Now = func() time.Time { return time.Date(2025, 3, 15, 10, 30, 0, 0, time.Local) }
	})
}

var simItemsParams = map[string]string{
	"input": sampleCSV, "engineers": "3", "days": "30", "sample-start": "2025-01-01", "sample-end": "2025-03-15",
	"target-start-date": "2025-03-16", "random-seed": "42", "simulations": "2000", "goroutines": "4",
}

func TestMeta_CoversEveryFlag(t *testing.T) {
	s := fixedServer(t)
	for _, d := range registry {
		fs := newDiscardFlagSet(d.Name)
		d.newFlags(fs, s.opts.Now())
		seen := map[string]bool{}
		fs.VisitAll(func(f *flag.Flag) {
			seen[f.Name] = true
			m, ok := metaFor(d.ID, f.Name)
			if !ok {
				t.Errorf("%s: flag -%s has no UI decision (add it to meta.go)", d.ID, f.Name)
				return
			}
			if m.Hidden == "" && (m.Kind == "" || m.Section == "" || m.Label == "") {
				t.Errorf("%s: flag -%s has incomplete meta %+v", d.ID, f.Name, m)
			}
		})
		for name := range metaOverrides[d.ID] {
			if !seen[name] {
				t.Errorf("%s: override for nonexistent flag -%s", d.ID, name)
			}
		}
	}
	for id := range metaOverrides {
		if id != "linear-sync" && commandByID(id) == nil {
			t.Errorf("override for unknown command %s", id)
		}
	}
}

func TestCommands_Schema(t *testing.T) {
	s := fixedServer(t)
	var res struct {
		Commands []struct {
			ID     string `json:"id"`
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"commands"`
	}
	decode(t, do(s, "GET", "/api/commands", nil, nil), &res)
	var ids []string
	for _, c := range res.Commands {
		ids = append(ids, c.ID)
	}
	var want []string
	for _, d := range registry {
		want = append(want, d.ID)
	}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	for _, c := range res.Commands {
		if c.ID != "sim-items" {
			continue
		}
		names := map[string]bool{}
		for _, f := range c.Fields {
			names[f.Name] = true
		}
		for _, n := range []string{"engineers", "days", "confidence"} {
			if !names[n] {
				t.Errorf("sim-items missing field %s", n)
			}
		}
		for _, n := range []string{"db", "config", "stdin-format", "manifest"} {
			if names[n] {
				t.Errorf("sim-items exposes hidden field %s", n)
			}
		}
	}
}

func TestRun_ParamErrors(t *testing.T) {
	s := fixedServer(t)
	with := func(extra map[string]string) map[string]string {
		p := map[string]string{"input": sampleCSV, "engineers": "2"}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	cases := []struct {
		name   string
		id     string
		params map[string]string
		msg    string
		fields []string
	}{
		{"unknown param", "sim-items", with(map[string]string{"bogus": "1", "days": "3"}), "unknown parameter -bogus", []string{"bogus"}},
		{"hidden param", "sim-items", with(map[string]string{"config": "x.yaml", "days": "3"}), "-config is not available in the web UI", []string{"config"}},
		{"bad int", "sim-items", with(map[string]string{"days": "abc"}), `invalid value "abc" for -days:`, []string{"days"}},
		{"bad date", "sim-items", with(map[string]string{"days": "3", "sample-start": "yesterdayish"}), "invalid -sample-start date:", []string{"sample-start"}},
		{"days and end", "sim-items", with(map[string]string{"days": "3", "target-end-date": "2025-04-01"}), "", nil},
		{"stdin", "sim-items", with(map[string]string{"days": "3", "input": "-"}), "-input -: stdin is not available in the web UI; upload the file instead", []string{"input"}},
		{"unknown command", "nope", nil, `unknown command "nope"`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, evs := post(t, s, c.id, c.params)
			wantCode := 400
			if c.id == "nope" {
				wantCode = 404
			}
			if code != wantCode {
				t.Fatalf("status = %d, want %d", code, wantCode)
			}
			e := evs[0].Error
			if c.msg != "" && !strings.HasPrefix(e.Message, c.msg) {
				t.Errorf("message = %q, want prefix %q", e.Message, c.msg)
			}
			if c.fields != nil && strings.Join(e.Fields, ",") != strings.Join(c.fields, ",") {
				t.Errorf("fields = %v, want %v", e.Fields, c.fields)
			}
			if c.name == "days and end" && len(e.Fields) == 0 {
				t.Errorf("expected a field error, got %+v", e)
			}
		})
	}
}

func TestRun_StreamShape(t *testing.T) {
	s := fixedServer(t)
	code, evs := post(t, s, "sim-items", simItemsParams)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	var results, lastDone, lastTotal int
	prev := 0
	var res event
	for _, ev := range evs {
		switch ev.Type {
		case "progress":
			if ev.Done < prev {
				t.Errorf("progress went backwards: %d after %d", ev.Done, prev)
			}
			prev, lastDone, lastTotal = ev.Done, ev.Done, ev.Total
		case "result":
			results++
			res = ev
		}
	}
	if results != 1 || evs[len(evs)-1].Type != "result" {
		t.Fatalf("want exactly one terminal result, got %d (last %s)", results, evs[len(evs)-1].Type)
	}
	if lastDone != 2000 || lastTotal != 2000 {
		t.Errorf("last progress = %d/%d", lastDone, lastTotal)
	}
	if !strings.HasPrefix(res.CLI, "forecast sim items") {
		t.Errorf("cli = %q", res.CLI)
	}
	if rec := do(s, "GET", "/api/runs/"+res.RunID+"/export/text", nil, nil); rec.Code != 200 {
		t.Errorf("export: %d", rec.Code)
	}
	var dto struct {
		Run struct {
			Seed string `json:"seed"`
		} `json:"run"`
	}
	json.Unmarshal(res.Result, &dto)
	if dto.Run.Seed != "42" {
		t.Errorf("seed = %q", dto.Run.Seed)
	}
}

func TestRun_ExportMatchesText(t *testing.T) {
	s := fixedServer(t)
	_, evs := post(t, s, "sim-items", simItemsParams)
	res := evs[len(evs)-1]
	for _, f := range res.Exports {
		a := do(s, "GET", "/api/runs/"+res.RunID+"/export/"+f, nil, nil)
		b := do(s, "GET", "/api/runs/"+res.RunID+"/export/"+f, nil, nil)
		if a.Code != 200 || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
			t.Errorf("export %s not stable (code %d)", f, a.Code)
		}
		if !strings.Contains(a.Header().Get("Content-Disposition"), "attachment") {
			t.Errorf("export %s disposition = %q", f, a.Header().Get("Content-Disposition"))
		}
	}
	if got := do(s, "GET", "/api/runs/"+res.RunID+"/export/text", nil, nil).Body.String(); got != res.Text {
		t.Errorf("text export differs from result text")
	}
	if rec := do(s, "GET", "/api/runs/"+res.RunID+"/export/bogus", nil, nil); rec.Code != 404 {
		t.Errorf("unknown format: %d", rec.Code)
	}
	if rec := do(s, "GET", "/api/runs/rnope/export/text", nil, nil); rec.Code != 404 {
		t.Errorf("unknown run: %d", rec.Code)
	}
	// The manifest records the effective invocation.
	var m struct {
		Invocation struct{ Args []string }
	}
	decode(t, do(s, "GET", "/api/runs/"+res.RunID+"/export/manifest", nil, nil), &m)
	if len(m.Invocation.Args) == 0 || m.Invocation.Args[0] != "forecast" {
		t.Errorf("manifest args = %v", m.Invocation.Args)
	}
}

// concurrentParams returns a valid parameter set per command.
func concurrentParams() map[string]map[string]string {
	p := func(extra map[string]string) map[string]string {
		m := map[string]string{"input": sampleCSV, "sample-start": "2025-01-01", "sample-end": "2025-03-15",
			"random-seed": "7", "simulations": "300", "goroutines": "2"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	flow := func(extra map[string]string) map[string]string {
		m := map[string]string{"input": sampleCSV}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	return map[string]map[string]string{
		"sim-items":       p(map[string]string{"engineers": "2", "days": "20", "target-start-date": "2025-03-16"}),
		"sim-days":        p(map[string]string{"engineers": "2", "items": "10", "target-start-date": "2025-03-16"}),
		"sim-probability": p(map[string]string{"engineers": "2", "days": "20", "target-start-date": "2025-03-16", "items": "8"}),
		"sim-backtest":    p(map[string]string{"engineers": "2", "project": "Search Revamp", "target-end-date": "2025-03-31"}),
		"history":         flow(map[string]string{"end": "2025-03-15"}),
		"cfd":             flow(map[string]string{"start": "2025-01-01", "end": "2025-03-15"}),
		"aging":           flow(map[string]string{"sample-start": "2025-01-01", "sample-end": "2025-03-15", "show-completed": "true"}),
		"count":           flow(map[string]string{"updated-since": "2025-01-01"}),
		"check":           flow(nil),
	}
}

func TestRun_Concurrent(t *testing.T) {
	s := fixedServer(t)
	params := concurrentParams()
	var wg sync.WaitGroup
	errs := make(chan string, 100)
	for _, d := range registry {
		p, ok := params[d.ID]
		if !ok {
			t.Fatalf("no concurrent params for %s", d.ID)
		}
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body, _ := json.Marshal(map[string]any{"params": p})
				req := httptest.NewRequest("POST", "/api/run/"+d.ID, bytes.NewReader(body))
				req.Host = "localhost"
				req.Header.Set("X-Forecast-Client", "web")
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
				if rec.Code != 200 || !strings.Contains(lines[len(lines)-1], `"type":"result"`) {
					errs <- fmt.Sprintf("%s: %d %s", d.ID, rec.Code, rec.Body.String())
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestStore_Evicts(t *testing.T) {
	st := newRunStore()
	first := st.add("x", time.Now(), &runOutput{})
	for i := 0; i < maxStoredRuns; i++ {
		st.add("x", time.Now(), &runOutput{})
	}
	if _, ok := st.get(first); ok {
		t.Error("the first run should have been evicted")
	}
	if len(st.runs) != maxStoredRuns {
		t.Errorf("stored %d runs", len(st.runs))
	}
}

func TestInlineHTML_UsesVendoredPlotly(t *testing.T) {
	s := fixedServer(t)
	_, evs := post(t, s, "cfd", concurrentParams()["cfd"])
	res := evs[len(evs)-1]
	if res.Type != "result" {
		t.Fatalf("run failed: %+v", res.Error)
	}
	inline := do(s, "GET", "/api/runs/"+res.RunID+"/export/html?inline=1", nil, nil)
	if !strings.Contains(inline.Body.String(), plotlyPath) || strings.Contains(inline.Body.String(), "cdn.plot.ly") {
		t.Error("inline html should load the vendored Plotly and nothing from the CDN")
	}
	if !strings.Contains(inline.Header().Get("Content-Security-Policy"), "script-src 'self' 'unsafe-inline'") {
		t.Errorf("inline CSP = %q", inline.Header().Get("Content-Security-Policy"))
	}
	if !strings.HasPrefix(inline.Header().Get("Content-Disposition"), "inline") {
		t.Errorf("disposition = %q", inline.Header().Get("Content-Disposition"))
	}
	plain := do(s, "GET", "/api/runs/"+res.RunID+"/export/html", nil, nil)
	if !strings.Contains(plain.Body.String(), "https://cdn.plot.ly/plotly-"+plotlyVersion+".min.js") {
		t.Error("the plain download should keep the CDN script tag")
	}
}

func TestDTO_Aging(t *testing.T) {
	s := fixedServer(t)
	p := concurrentParams()["aging"]
	p["percentile"] = "70"
	_, evs := post(t, s, "aging", p)
	res := evs[len(evs)-1]
	if res.Type != "result" {
		t.Fatalf("run failed: %+v", res.Error)
	}
	var dto struct {
		Meta struct {
			HasThreshold bool   `json:"has_threshold"`
			Label        string `json:"label"`
		} `json:"meta"`
		InProgress []struct {
			AgeClass        string   `json:"age_class"`
			Multiplier      *float64 `json:"multiplier"`
			MultiplierLabel string   `json:"multiplier_label"`
			PercentileLabel string   `json:"percentile_label"`
		} `json:"in_progress"`
		Completed []struct {
			AgeClass string `json:"age_class"`
		} `json:"completed"`
		CycleTimes []float64 `json:"cycle_times"`
	}
	if err := json.Unmarshal(res.Result, &dto); err != nil {
		t.Fatal(err)
	}
	if len(dto.InProgress) == 0 || len(dto.Completed) == 0 || len(dto.CycleTimes) == 0 || dto.Meta.Label != "P70" {
		t.Fatalf("dto = %+v", dto)
	}
	for _, it := range dto.InProgress {
		switch it.AgeClass {
		case "high", "medium", "normal":
		default:
			t.Errorf("age_class = %q", it.AgeClass)
		}
		want := "—"
		if dto.Meta.HasThreshold && it.Multiplier != nil {
			want = fmt.Sprintf("%.2fx", *it.Multiplier)
		}
		if it.MultiplierLabel != want {
			t.Errorf("multiplier_label = %q, want %q", it.MultiplierLabel, want)
		}
		if !strings.HasSuffix(it.PercentileLabel, "th") && !strings.HasSuffix(it.PercentileLabel, "st") &&
			!strings.HasSuffix(it.PercentileLabel, "nd") && !strings.HasSuffix(it.PercentileLabel, "rd") {
			t.Errorf("percentile_label = %q", it.PercentileLabel)
		}
	}

	// Without -show-completed, completed is null.
	p2 := concurrentParams()["aging"]
	delete(p2, "show-completed")
	_, evs = post(t, s, "aging", p2)
	var d2 struct {
		Completed []any `json:"completed"`
	}
	json.Unmarshal(evs[len(evs)-1].Result, &d2)
	if d2.Completed != nil {
		t.Errorf("completed = %v, want null", d2.Completed)
	}
}
