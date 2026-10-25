package params

import (
	"errors"
	"flag"
	"strings"

	"gopkg.in/yaml.v3"
)

// isBoolFlag reports whether f is a boolean flag.
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// CommandLine returns the argv equivalent to fs's explicitly set flags, in
// fs.VisitAll (sorted) order, prefixed by "forecast" and the command name
// split on spaces. Bool flags render as -name when true and -name=false
// when false. -config is never included.
func CommandLine(name string, fs *flag.FlagSet) []string {
	argv := append([]string{"forecast"}, strings.Fields(name)...)
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "config" || !IsSet(fs, f.Name) {
			return
		}
		v := f.Value.String()
		switch {
		case isBoolFlag(f) && v == "true":
			argv = append(argv, "-"+f.Name)
		case isBoolFlag(f):
			argv = append(argv, "-"+f.Name+"=false")
		default:
			argv = append(argv, "-"+f.Name, v)
		}
	})
	return argv
}

// ShellJoin quotes args for a POSIX shell: an arg made only of
// [A-Za-z0-9_./:=,@%+-] is left bare; anything else is single-quoted with
// embedded ' written as '\”.
func ShellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

func shellQuote(a string) string {
	if a != "" && strings.IndexFunc(a, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:=,@%+-", r))
	}) < 0 {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}

// ConfigYAML renders fs's explicitly set flags (except -config) as a
// -config YAML document: a map of flag name → string value, marshaled
// with gopkg.in/yaml.v3 (which sorts keys).
func ConfigYAML(fs *flag.FlagSet) (string, error) {
	m := map[string]string{}
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name != "config" && IsSet(fs, f.Name) {
			m[f.Name] = f.Value.String()
		}
	})
	b, err := yaml.Marshal(m)
	return string(b), err
}

// SplitCommandLine splits a pasted shell command line into arguments,
// handling single quotes (literal), double quotes (backslash escapes \" \\ \$
// and \`) and backslash escapes outside quotes. An unterminated quote is an
// error.
func SplitCommandLine(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	have := false
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j >= len(rs) {
				return nil, errors.New("unterminated single quote")
			}
			cur.WriteString(string(rs[i+1 : j]))
			have = true
			i = j
		case c == '"':
			have = true
			i++
			for ; i < len(rs) && rs[i] != '"'; i++ {
				if rs[i] == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]) {
					i++
				}
				cur.WriteRune(rs[i])
			}
			if i >= len(rs) {
				return nil, errors.New("unterminated double quote")
			}
		case c == '\\':
			if i+1 >= len(rs) {
				return nil, errors.New("trailing backslash")
			}
			i++
			cur.WriteRune(rs[i])
			have = true
		case c == ' ' || c == '\t' || c == '\n':
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(c)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args, nil
}
