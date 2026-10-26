package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func textExport(write func(w io.Writer) error) export {
	return export{ContentType: "text/plain; charset=utf-8", Ext: ".txt", Write: write}
}
func csvExport(write func(w io.Writer) error) export {
	return export{ContentType: "text/csv; charset=utf-8", Ext: ".csv", Write: write}
}
func htmlExport(write func(w io.Writer) error) export {
	return export{ContentType: "text/html; charset=utf-8", Ext: ".html", Write: write}
}

// encodeIndented writes v as JSON indented by two spaces, like the CLI's
// manifest writer.
func encodeIndented(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonExport renders v indented by two spaces.
func jsonExport(v any) export {
	return export{ContentType: "application/json", Ext: ".json", Write: func(w io.Writer) error {
		return encodeIndented(w, v)
	}}
}

// rawJSONExport is for exports whose JSON comes from a renderer (the CLI's
// own JSON output) rather than an encoder.
func rawJSONExport(write func(w io.Writer) error) export {
	return export{ContentType: "application/json", Ext: ".json", Write: write}
}

const inlineCSP = "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:"

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	run, ok := s.runs.get(r.PathValue("run_id"))
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown run"), nil)
		return
	}
	format := r.PathValue("format")
	ex, ok := run.Out.Exports[format]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("format %q not available for this run", format), nil)
		return
	}
	var buf bytes.Buffer
	if err := ex.Write(&buf); err != nil {
		writeError(w, http.StatusInternalServerError, err, nil)
		return
	}
	body := buf.Bytes()

	inline := r.URL.Query().Get("inline") == "1"
	if inline && format == "html" {
		body = bytes.ReplaceAll(body,
			[]byte(`<script src="https://cdn.plot.ly/plotly-`+plotlyVersion+`.min.js"></script>`),
			[]byte(`<script src="`+plotlyPath+`"></script>`))
		w.Header().Set("Content-Security-Policy", inlineCSP)
	}
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", ex.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="forecast-%s-%s%s"`, disposition, run.Command, run.ID, ex.Ext))
	w.Write(body)
}
