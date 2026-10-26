package main

import "testing"

func TestValidateServeFlags(t *testing.T) {
	cases := []struct {
		name string
		f    serveFlags
		want string
	}{
		{"default loopback", serveFlags{addr: "127.0.0.1:8080"}, ""},
		{"localhost", serveFlags{addr: "localhost:8080"}, ""},
		{"ipv6 loopback", serveFlags{addr: "[::1]:8080"}, ""},
		{"all interfaces", serveFlags{addr: ":8080"}, "-addr :8080 is not a loopback address; pass -allow-remote to listen on it"},
		{"non-loopback", serveFlags{addr: "0.0.0.0:8080"}, "-addr 0.0.0.0:8080 is not a loopback address; pass -allow-remote to listen on it"},
		{"remote needs data dir", serveFlags{addr: "0.0.0.0:8080", allowRemote: true}, "-allow-remote requires -data-dir"},
		{"remote ok", serveFlags{addr: "0.0.0.0:8080", allowRemote: true, dataDir: "/srv"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateServeFlags(c.f)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
