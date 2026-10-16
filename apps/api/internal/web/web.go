// Package web serves the dashboard: the static export of apps/dashboard plus /api/*
// streamed to the API. The API's local-mode checks see what they always saw
// from the dashboard: X-Forwarded-Host set to the host the browser used, and
// client-supplied forwarding headers dropped.
package web

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"strings"
)

// MaxProxyBodyBytes caps proxied request bodies (the API's default upload limit is 100 MB).
const MaxProxyBodyBytes = 110 << 20

// Inline scripts/styles: Next's bootstrap and next-themes' no-flash script are
// inline, and a static export cannot use nonces.
var securityHeaders = map[string]string{
	"Content-Security-Policy": strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		// External item media (item.url) may live on any host.
		"img-src 'self' data: blob: https: http:",
		"media-src 'self' blob: https: http:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; "),
	"X-Frame-Options":        "DENY",
	"Referrer-Policy":        "no-referrer",
	"X-Content-Type-Options": "nosniff",
	"Permissions-Policy":     "camera=(), microphone=(), geolocation=(), interest-cohort=()",
}

// Pages with an [id] segment are exported once, as id "_".
var idRoutes = map[string]bool{"items": true, "collections": true}

// RelayHeader carries relayToken on every proxied request: the API in the same process (staqly serve, the
// image) takes the browser's address from X-Forwarded-For only on requests that bear it.
const RelayHeader = "X-Staqly-Relay"

// Handler serves files from site (the root of the static export) and proxies
// /api/* to the API at upstream (e.g. http://api:3333). trustProxy (TRUST_PROXY)
// takes the browser's address from the right-most X-Forwarded-For entry set by
// your own reverse proxy instead of the TCP peer. relayToken ("" = none) is a
// secret shared with an API in the same process, sent as RelayHeader.
func Handler(site fs.FS, upstream *url.URL, log *slog.Logger, trustProxy bool, relayToken string) http.Handler {
	mux := http.NewServeMux()
	api := apiProxy(upstream, log, trustProxy, relayToken)
	mux.Handle("/api/", api)
	mux.Handle("/api", api)
	mux.Handle("/", files(site))
	return mux
}

// DialURL is the URL this process uses to reach a server listening on addr
// (an unspecified host means this machine).
func DialURL(addr string) *url.URL {
	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
			host = "127.0.0.1"
		}
		addr = net.JoinHostPort(host, port)
	}
	return &url.URL{Scheme: "http", Host: addr}
}

// ClientIP is the browser's address as the dashboard sees it: the TCP peer, or with trustProxy the
// right-most X-Forwarded-For entry (the one your own proxy appended; anything left of it is
// client-controlled). "" if neither parses.
func ClientIP(r *http.Request, trustProxy bool) string {
	if xff := r.Header.Values("X-Forwarded-For"); trustProxy && len(xff) > 0 {
		parts := strings.Split(xff[len(xff)-1], ",")
		if ip, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
			return ip.Unmap().String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String()
	}
	return ""
}

func apiProxy(upstream *url.URL, log *slog.Logger, trustProxy bool, relayToken string) http.Handler {
	rp := &httputil.ReverseProxy{
		// Rewrite (unlike Director) already drops the client's X-Forwarded-*
		// and hop-by-hop headers.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			p := strings.TrimPrefix(pr.In.URL.Path, "/api")
			if p == "" {
				p = "/"
			}
			pr.Out.URL.Path = strings.TrimSuffix(upstream.Path, "/") + p
			pr.Out.URL.RawPath = ""
			pr.Out.Header.Del("Forwarded")
			pr.Out.Header.Del("X-Real-Ip")
			// Never empty: the API treats the presence of X-Forwarded-Host as
			// "relayed by the dashboard" and its value as the host the browser used.
			host := strings.TrimSpace(pr.In.Host)
			if host == "" {
				host = "invalid"
			}
			pr.Out.Header.Set("X-Forwarded-Host", host)
			proto := "http"
			if pr.In.TLS != nil {
				proto = "https"
			}
			pr.Out.Header.Set("X-Forwarded-Proto", proto)
			// The browser's address, so the API's per-IP limits (sign-up, recovery, connect links) and logs
			// see each browser instead of one shared loopback peer. Set from the peer (or the trusted
			// proxy's entry), never copied from what the browser sent.
			if ip := ClientIP(pr.In, trustProxy); ip != "" {
				pr.Out.Header.Set("X-Forwarded-For", ip)
			}
			pr.Out.Header.Del(RelayHeader) // never the browser's
			if relayToken != "" {
				pr.Out.Header.Set(RelayHeader, relayToken)
			}
		},
		FlushInterval: -1, // long-polls, MCP streams and downloads go out as they come
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var tooBig *http.MaxBytesError
			switch {
			case errors.As(err, &tooBig):
				errorJSON(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
			case r.Context().Err() != nil: // the browser went away
			default:
				log.Error("api proxy", "method", r.Method, "path", r.URL.Path, "err", err)
				errorJSON(w, http.StatusBadGateway, "bad_gateway", "The staqly API is not reachable.")
			}
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > MaxProxyBodyBytes {
			errorJSON(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxProxyBodyBytes)
		rp.ServeHTTP(w, r)
	})
}

func errorJSON(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func files(site fs.FS) http.Handler {
	_, err := fs.Stat(site, "index.html")
	built := err == nil
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range securityHeaders {
			w.Header().Set(k, v)
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !built {
			http.Error(w, "The dashboard is not built into this binary: run `make dashboard` first.", http.StatusServiceUnavailable)
			return
		}
		name, ok := resolve(site, r.URL.Path)
		status := http.StatusOK
		if !ok {
			name, status = "404.html", http.StatusNotFound
		}
		if strings.HasPrefix(name, "_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed names
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		serve(w, r, site, name, status)
	})
}

// resolve maps a URL path onto a file of the export: / → index.html,
// /inbox → inbox.html, /items/itm_1 → items/_.html (and its .txt RSC payloads).
func resolve(site fs.FS, urlPath string) (string, bool) {
	p := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if p == "" {
		p = "index"
	}
	try := func(p string) (string, bool) {
		for _, c := range []string{p, p + ".html", path.Join(p, "index.html")} {
			if st, err := fs.Stat(site, c); err == nil && !st.IsDir() {
				return c, true
			}
		}
		return "", false
	}
	if name, ok := try(p); ok {
		return name, true
	}
	segs := strings.Split(p, "/")
	if len(segs) >= 2 && idRoutes[segs[0]] {
		ext := ""
		if i := strings.Index(segs[1], "."); i >= 0 {
			ext = segs[1][i:]
		}
		segs[1] = "_" + ext
		return try(strings.Join(segs, "/"))
	}
	return "", false
}

func serve(w http.ResponseWriter, r *http.Request, site fs.FS, name string, status int) {
	f, err := site.Open(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	if status != http.StatusOK { // ServeContent always answers 200/206/304
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, f)
		}
		return
	}
	rs, ok := f.(io.ReadSeeker)
	st, serr := f.Stat()
	if !ok || serr != nil {
		_, _ = io.Copy(w, f)
		return
	}
	http.ServeContent(w, r, name, st.ModTime(), rs)
}
