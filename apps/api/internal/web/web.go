// Package web serves the dashboard: the static export of apps/platform plus /api/*
// streamed to the API. The API's local-mode checks see what they always saw
// from the dashboard: X-Forwarded-Host set to the host the browser used, and
// client-supplied forwarding headers dropped.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
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

// The media editor (/media/edit, OpenGPEX in apps/platform/lib/opengpex) has its codecs and workers at /ext.
// Its wasm codecs need cross-origin isolation (COOP + COEP), and its on-device AI tools load ONNX Runtime and
// transformers.js (pinned versions, jsDelivr) and download model weights from Hugging Face when someone uses
// them. Text layers may load Google Fonts. Nothing else leaves the page.
//
// jsDelivr is allowed per package version only: a CSP source path without a trailing "/" matches that exact
// path, and with one only what lies under it, so scripts and wasm from any other npm package stay blocked.
// The versions: ORT_VERSION (ort-session.ts), transformers.js (tfm-session.ts) and the ONNX Runtime build that
// transformers.js 4.2.0 loads by itself; apps/platform/tests/editor-csp.test.ts checks the first two.
const editorCDN = "https://cdn.jsdelivr.net/npm/onnxruntime-web@1.27.0/ " +
	"https://cdn.jsdelivr.net/npm/onnxruntime-web@1.26.0-dev.20260416-b7804b056c/ " +
	"https://cdn.jsdelivr.net/npm/@huggingface/transformers@4.2.0 " +
	"https://cdn.jsdelivr.net/npm/@huggingface/transformers@4.2.0/"

var editorHeaders = map[string]string{
	"Content-Security-Policy": strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval' " + editorCDN,
		"worker-src 'self' blob:",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"img-src 'self' data: blob:",
		"media-src 'self' blob:",
		"font-src 'self' data: https://fonts.gstatic.com",
		"connect-src 'self' blob: data: https://huggingface.co https://*.huggingface.co https://*.hf.co " + editorCDN + " https://fonts.googleapis.com https://fonts.gstatic.com https://www.googleapis.com",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; "),
	"Cross-Origin-Opener-Policy":   "same-origin",
	"Cross-Origin-Embedder-Policy": "require-corp",
	"Cross-Origin-Resource-Policy": "same-origin",
}

// editorPath: the editor's page and files (see editorHeaders). The editor's workers are bundled chunks under
// /_next/static: a worker of an isolated page needs the same headers (on other script files they do nothing).
func editorPath(p string) bool {
	return p == "/media/edit" || strings.HasPrefix(p, "/media/edit/") || strings.HasPrefix(p, "/ext/") ||
		strings.HasPrefix(p, "/_next/static/")
}

// Pages with an [id] segment are exported once, as id "_": the route prefix and the segment that holds
// the id (0-based): /posts/post_1 → posts/_, /channels/callback/telegram → channels/callback/_.
var idRoutes = map[string]int{"posts": 1, "channels/callback": 2}

// moved: routes of earlier dashboards, for old bookmarks. /collections/<id> → /posts?collection=<id>.
var moved = map[string]string{"/inbox": "/schedule", "/notifications": "/alerts", "/collections": "/posts"}

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
	// The OAuth redirect URL people register at a platform is the dashboard's callback page: while the
	// platform's answer (code or state, or an error) is on it, hand it to the API, which finishes the
	// connection and sends the browser back here without those parameters to land.
	page := files(site)
	mux.HandleFunc("/channels/callback/", func(rw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		provider := strings.TrimPrefix(r.URL.Path, "/channels/callback/")
		if provider != "" && !strings.Contains(provider, "/") && (q.Has("code") || q.Has("state") || q.Has("error")) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/api/v1/channels/oauth/" + provider + "/callback"
			if linkState(q.Get("state")) {
				// Started from a connect link by someone without a session: the link's own callback.
				r2.URL.Path = "/api/v1/connect-links/oauth/" + provider + "/callback"
			}
			r2.URL.RawPath = ""
			api.ServeHTTP(rw, r2)
			return
		}
		page.ServeHTTP(rw, r)
	})
	for from, to := range moved {
		mux.Handle(from, http.RedirectHandler(to, http.StatusMovedPermanently))
	}
	mux.HandleFunc("/collections/", func(rw http.ResponseWriter, r *http.Request) {
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/collections/"), "/")
		if id == "" || strings.Contains(id, "/") {
			page.ServeHTTP(rw, r)
			return
		}
		http.Redirect(rw, r, "/posts?collection="+url.QueryEscape(id), http.StatusMovedPermanently)
	})
	mux.Handle("/", page)
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
	assets := &assetCache{files: map[string]*asset{}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range securityHeaders {
			w.Header().Set(k, v)
		}
		if editorPath(r.URL.Path) {
			for k, v := range editorHeaders {
				w.Header().Set(k, v)
			}
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
		if status == http.StatusOK {
			if a := assets.get(site, name); a != nil {
				serveAsset(w, r, name, a)
				return
			}
		}
		serve(w, r, site, name, status)
	})
}

// asset is a file of the export held in memory with its ETag and, for text, its gzipped form.
type asset struct {
	body, gz []byte
	etag     string
}

// assetCache fills on first request. The export is embedded and never changes while the process runs, and
// it is a few MB, so every file is read, hashed and compressed once. Pages are revalidated (no-cache) on
// each navigation and the RSC payloads (.txt) on each client-side one: the ETag turns those into 304s.
type assetCache struct {
	mu    sync.Mutex
	files map[string]*asset
}

// compressible: what a browser fetches as text (pages, RSC payloads, scripts, styles, SVG, manifests).
var compressible = map[string]bool{".html": true, ".txt": true, ".js": true, ".css": true, ".svg": true,
	".json": true, ".webmanifest": true, ".map": true, ".xml": true, ".ico": true, ".mjs": true, ".wasm": true}

func (c *assetCache) get(site fs.FS, name string) *asset {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.files[name]; ok {
		return a
	}
	body, err := fs.ReadFile(site, name)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(body)
	a := &asset{body: body, etag: `"` + hex.EncodeToString(sum[:12]) + `"`}
	if compressible[path.Ext(name)] && len(body) > 512 {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(body)
		if zw.Close() == nil && buf.Len() < len(body) {
			a.gz = buf.Bytes()
		}
	}
	c.files[name] = a
	return a
}

func serveAsset(w http.ResponseWriter, r *http.Request, name string, a *asset) {
	body, etag := a.body, a.etag
	if a.gz != nil {
		w.Header().Add("Vary", "Accept-Encoding")
		if acceptsGzip(r) {
			body, etag = a.gz, strings.TrimSuffix(a.etag, `"`)+`-gz"`
			w.Header().Set("Content-Encoding", "gzip")
			// ServeContent leaves Content-Length out once an encoding is set (it expects compression on
			// the fly); this body is final, so send its length, and always whole (no byte ranges of gzip).
			r.Header.Del("Range")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
	}
	if w.Header().Get("Content-Type") == "" {
		ctype := mime.TypeByExtension(path.Ext(name))
		if ctype == "" {
			ctype = http.DetectContentType(a.body)
		}
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("ETag", etag) // ServeContent answers If-None-Match with 304
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}

// acceptsGzip: "gzip" listed in Accept-Encoding without q=0.
func acceptsGzip(r *http.Request) bool {
	for _, v := range r.Header.Values("Accept-Encoding") {
		for _, part := range strings.Split(v, ",") {
			coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
			if !strings.EqualFold(strings.TrimSpace(coding), "gzip") && strings.TrimSpace(coding) != "*" {
				continue
			}
			q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
			if q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000" {
				return true
			}
		}
	}
	return false
}

// resolve maps a URL path onto a file of the export: / → index.html,
// /schedule → schedule.html, /posts/post_1 → posts/_.html (and its .txt RSC payloads).
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
	for prefix, at := range idRoutes {
		if len(segs) > at && strings.Join(segs[:at], "/") == prefix {
			ext := ""
			if i := strings.Index(segs[at], "."); i >= 0 {
				ext = segs[at][i:]
			}
			segs[at] = "_" + ext
			return try(strings.Join(segs, "/"))
		}
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

// linkState tells whether an OAuth state was issued for a connect link (its signed body names the link). Only for
// routing: the API checks the signature.
func linkState(state string) bool {
	body, _, ok := strings.Cut(state, ".")
	if !ok {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return false
	}
	var st struct {
		Link string `json:"l"`
	}
	return json.Unmarshal(raw, &st) == nil && st.Link != ""
}
