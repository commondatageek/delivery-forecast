package logx

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	w.Close()
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestInfofPlainLine(t *testing.T) {
	colorOn = false
	SetLevel(Info)

	out := captureStderr(t, func() {
		Infof("team=%s count=%d", "ENG", 3)
	})

	if !strings.Contains(out, "INFO") {
		t.Errorf("expected output to contain level label INFO, got %q", out)
	}
	if !strings.Contains(out, "team=ENG count=3") {
		t.Errorf("expected output to contain formatted message, got %q", out)
	}
	if strings.Contains(out, "\033[") {
		t.Errorf("expected no ANSI escape codes with color disabled, got %q", out)
	}
}

func TestDebugfSuppressedByDefault(t *testing.T) {
	colorOn = false
	SetLevel(Info)

	out := captureStderr(t, func() {
		Debugf("should not appear")
	})

	if out != "" {
		t.Errorf("expected Debugf to be suppressed at Info threshold, got %q", out)
	}
}
