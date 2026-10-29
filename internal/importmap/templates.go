package importmap

import (
	"embed"
	"slices"
	"strings"
)

// templateFS holds the starter mappings `forecast import -example` prints.
// They ship in the binary because the people who need them install a
// release, not the repo.
//
//go:embed templates/*.yaml
var templateFS embed.FS

// Templates lists the embedded templates' names (file names without
// .yaml), sorted.
func Templates() []string {
	entries, _ := templateFS.ReadDir("templates")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	slices.Sort(names)
	return names
}

// Template returns an embedded template's YAML text, verbatim.
func Template(name string) ([]byte, bool) {
	if !slices.Contains(Templates(), name) {
		return nil, false
	}
	data, err := templateFS.ReadFile("templates/" + name + ".yaml")
	return data, err == nil
}

// Describe returns a template's display name: its first line,
// "# name: <display name>". It is "" for an unknown template.
func Describe(name string) string {
	data, ok := Template(name)
	if !ok {
		return ""
	}
	first, _, _ := strings.Cut(string(data), "\n")
	if v, ok := strings.CutPrefix(first, "# name:"); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// Suggest returns the first template (in Templates() order) whose detect
// headers are all present in header, compared as Apply compares headers.
// It is how a raw export passed to -input gets pointed at forecast import.
func Suggest(header []string) (name string, ok bool) {
	have := make(map[string]bool, len(header))
	for _, h := range header {
		have[norm(h)] = true
	}
	for _, name := range Templates() {
		data, _ := Template(name)
		m, err := Parse(data)
		if err != nil || len(m.Detect) == 0 {
			continue
		}
		if !slices.ContainsFunc(m.Detect, func(d string) bool { return !have[norm(d)] }) {
			return name, true
		}
	}
	return "", false
}
