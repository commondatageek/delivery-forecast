package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/commondatageek/delivery-forecast/internal/logx"
	"github.com/commondatageek/delivery-forecast/internal/util"
	"github.com/commondatageek/delivery-forecast/internal/web"
)

// serveFlags holds the parsed flags of `forecast serve`.
type serveFlags struct {
	addr        string
	input       string
	exclusions  string
	uploadDir   string
	maxUploadMB int64
	allowRemote bool
	dataDir     string
	open        bool
}

// validateServeFlags checks the combinations of flags that would expose the
// server beyond the local machine unsafely.
func validateServeFlags(f serveFlags) error {
	if !f.allowRemote && !web.LoopbackAddr(f.addr) {
		return fmt.Errorf("-addr %s is not a loopback address; pass -allow-remote to listen on it", f.addr)
	}
	if f.allowRemote && f.dataDir == "" {
		return fmt.Errorf("-allow-remote requires -data-dir")
	}
	return nil
}

func cmdServe(args []string) error {
	cmd := flag.NewFlagSet("serve", flag.ExitOnError)
	var f serveFlags
	cmd.StringVar(&f.addr, "addr", "127.0.0.1:8080", "listen address")
	cmd.StringVar(&f.input, "input", "", "default data source pre-filled in the UI")
	cmd.StringVar(&f.exclusions, "exclusions", "", "default exclusions file pre-filled in sim forms and check")
	cmd.StringVar(&f.uploadDir, "upload-dir", "", "where uploads are stored (default: <user cache dir>/delivery-forecast/uploads)")
	cmd.Int64Var(&f.maxUploadMB, "max-upload-mb", 512, "upload size limit in MiB")
	cmd.BoolVar(&f.allowRemote, "allow-remote", false, "allow a non-loopback -addr (requires -data-dir)")
	cmd.StringVar(&f.dataDir, "data-dir", "", "with -allow-remote: the only directory paths may point into")
	cmd.BoolVar(&f.open, "open", false, "open the UI in the default browser once the server is listening")
	configFile := cmd.String("config", "", "path to a YAML config file supplying flag values (CLI flags override)")
	cmd.Parse(args)

	if err := util.ApplyConfig(cmd, *configFile); err != nil {
		return err
	}
	if err := validateServeFlags(f); err != nil {
		return err
	}

	if f.uploadDir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("find upload directory (pass -upload-dir): %w", err)
		}
		f.uploadDir = filepath.Join(cache, "delivery-forecast", "uploads")
	}

	v := version
	if v == "" {
		v = "(dev)"
	}
	srv, err := web.New(web.Options{
		DefaultInput:      f.input,
		DefaultExclusions: f.exclusions,
		UploadDir:         f.uploadDir,
		MaxUploadBytes:    f.maxUploadMB << 20,
		AllowRemote:       f.allowRemote,
		DataDir:           f.dataDir,
		Binary:            buildInfo(),
		Version:           v,
		Now:               nowFunc,
		Logf:              func(format string, args ...any) { logx.Infof(format, args...) },
	})
	if err != nil {
		return err
	}

	// Listen before logging so a port conflict fails fast.
	ln, err := net.Listen("tcp", f.addr)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/", ln.Addr())
	logx.Infof("forecast web UI: %s", url)
	if f.open {
		if err := openBrowser(url); err != nil {
			logx.Warnf("could not open a browser: %v", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hs := &http.Server{Handler: srv.Handler()}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}
