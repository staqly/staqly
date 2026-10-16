// Command dashboard serves the dashboard on its own (the staqly-dashboard image): the
// embedded static export of apps/dashboard, with /api/* streamed to the API.
//
//	ADDR              listen address (default :8080)
//	API_INTERNAL_URL  where the API is reachable (default http://api:3333)
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/staqly/staqly/apps/api"
	"github.com/staqly/staqly/apps/api/internal/logctx"
	"github.com/staqly/staqly/apps/api/internal/web"
)

var version = "dev"

func main() {
	// `web health` is the image's HEALTHCHECK (no wget/curl in it).
	if len(os.Args) > 1 && os.Args[1] == "health" {
		res, err := http.Get("http://127.0.0.1" + port(env("ADDR", ":8080")) + "/login")
		if err != nil || res.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	log := logctx.Base(logctx.New(os.Stdout, env("LOG_FORMAT", "json"), slog.LevelInfo), version, "web")
	upstream, err := url.Parse(env("API_INTERNAL_URL", "http://api:3333"))
	if err != nil || upstream.Host == "" {
		fmt.Fprintln(os.Stderr, "web: invalid API_INTERNAL_URL")
		os.Exit(2)
	}
	site, _ := fs.Sub(api.Web, "web")
	// No WriteTimeout: long-polls, MCP streams and downloads pass through.
	srv := &http.Server{Addr: env("ADDR", ":8080"), Handler: web.Handler(site, upstream, log, env("TRUST_PROXY", "") == "true", ""),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Minute, IdleTimeout: 120 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "api", upstream.String())
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("fatal", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// port returns ":8080" for ":8080", "0.0.0.0:8080" or "[::]:8080".
func port(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i:]
		}
	}
	return ":" + addr
}
