package main

import (
    "embed"
    "io/fs"
    "log/slog"
    "net"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/gorilla/mux"
    "leblanc.io/open-go-base/appconf"
    "leblanc.io/open-go-base/ratelimit"
)

// assetsFS carries the stylesheets, scripts, fonts and images inside the binary.
// Everything the service needs to run is in there; only the paste storage and,
// when there is one, the operator stylesheet live outside.
//
// The stylesheets are listed one by one on purpose: "assets/css" would also pick
// up the git ignored custom.css of whoever runs the build, and ship it in the
// release. TestTheEmbeddedAssetsMatchTheAssetsFolder guards the list.
//
//go:embed assets/js assets/img assets/fonts assets/css/highlightjs
//go:embed assets/css/about.css assets/css/app.css assets/css/color.css assets/css/error.css
var assetsFS embed.FS

// customCssName is the one asset deliberately left out of the binary.
const customCssName = "assets/css/custom.css"

// maxRequestBytes caps the whole request body. Pastes are posted as a comma
// separated list of decimal bytes, so roughly four characters per byte of clear
// text: this leaves about one megabyte of text. Without it ParseMultipartForm
// spills to disk past 32 MB with no global ceiling at all.
const maxRequestBytes = 4 << 20

func getServerParameters(web appconf.Web) string {
    return net.JoinHostPort(web.Host, strconv.Itoa(web.Port))
}

// overlayFileSystem serves the embedded assets, with a folder on disk taking
// precedence over them. This is what keeps OPEN_GO_PASTE_CUSTOM_CSS usable: the
// operator stylesheet is written at deployment time, it cannot be part of the
// binary. It also lets the front end be edited without rebuilding.
type overlayFileSystem struct {
    embedded http.FileSystem
    overlay  http.FileSystem
}

func (o overlayFileSystem) Open(name string) (http.File, error) {
    if nil != o.overlay {
        file, err := o.overlay.Open(name)
        if nil == err {
            return file, nil
        }
    }

    return o.embedded.Open(name)
}

// assetsOverlay is the optional folder overriding the embedded assets, empty
// when the operator did not configure one.
func assetsOverlay() http.FileSystem {
    folder := config.Paste.AssetsFolder
    if "" == folder {
        return nil
    }

    return http.Dir(folder)
}

func embeddedAssets() http.FileSystem {
    // The embedded tree is rooted at "assets", the urls are not.
    assets, err := fs.Sub(assetsFS, "assets")
    if nil != err {
        // Only reachable if the embed directive and this path disagree.
        panic(err)
    }

    return http.FS(assets)
}

func assetsDelivery(r *mux.Router) {
    fileSystem := overlayFileSystem{
        embedded: embeddedAssets(),
        overlay:  assetsOverlay(),
    }

    r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(fileSystem)))
}

// checkCustomCss traces a stylesheet that will 404 at the first page load: with
// the assets embedded, a "/static/..." path only resolves if the file is either
// in the binary or in OPEN_GO_PASTE_ASSETS_FOLDER.
func checkCustomCss() {
    customCss := config.Paste.CustomCss
    if "" == customCss || !strings.HasPrefix(customCss, "/static/") {
        return
    }

    name := strings.TrimPrefix(customCss, "/static/")

    file, err := overlayFileSystem{
        embedded: embeddedAssets(),
        overlay:  assetsOverlay(),
    }.Open("/" + name)

    if nil != err {
        slog.Warn(
            "custom CSS is served by no asset: put the file in OPEN_GO_PASTE_ASSETS_FOLDER, or use an absolute url",
            "css", customCss,
        )
        return
    }

    file.Close()
}

// limitBodySize rejects oversized requests before they reach any controller. A
// truncated body makes ParseMultipartForm fail, the paste content no longer
// matches its pattern and the client gets a 400.
func limitBodySize(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

        next.ServeHTTP(w, r)
    })
}

// limitRequestRate throttles everything but the static assets. Creating a paste
// is anonymous and writes a file on disk, so nothing but a quota stops a single
// client from filling the storage. A page load pulls a dozen assets along the
// way, and counting those would burn a per-minute budget meant for the pastes
// themselves within a few views.
func limitRequestRate(limiter *ratelimit.Limiter, next http.Handler) http.Handler {
    throttled := limiter.Middleware(next)

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/static/") {
            next.ServeHTTP(w, r)
            return
        }

        throttled.ServeHTTP(w, r)
    })
}

// styleSources allows the operator provided stylesheet when it is hosted
// elsewhere, without opening style-src any wider.
func styleSources() string {
    sources := "'self'"

    customCss := config.Paste.CustomCss
    if "" == customCss {
        return sources
    }

    parsed, err := url.Parse(customCss)
    if nil != err || !parsed.IsAbs() || "" == parsed.Host {
        return sources
    }

    return sources + " " + parsed.Scheme + "://" + parsed.Host
}

// contentSecurityPolicy stays free of 'unsafe-inline' for scripts, which is what
// makes it a real mitigation of an injection in the decrypted markdown: every
// script is served from /static, the paste data reaches app.js through data-*
// attributes rather than an inline block.
func contentSecurityPolicy() string {
    return strings.Join([]string{
        "default-src 'none'",
        "script-src 'self'",
        "style-src " + styleSources(),
        "img-src 'self'",
        "font-src 'self'",
        "manifest-src 'self'",
        "connect-src 'self'",
        "form-action 'self'",
        "base-uri 'none'",
        "frame-ancestors 'none'",
    }, "; ")
}

func securityHeaders(next http.Handler) http.Handler {
    csp := contentSecurityPolicy()

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        header := w.Header()
        header.Set("Content-Security-Policy", csp)
        header.Set("X-Content-Type-Options", "nosniff")
        header.Set("X-Frame-Options", "DENY")
        header.Set("Referrer-Policy", "no-referrer")

        // A paste must not survive in the browser disk cache: a single read one
        // would still be readable there long after its destruction. Static
        // assets are the only responses worth caching.
        if !strings.HasPrefix(r.URL.Path, "/static/") {
            header.Set("Cache-Control", "no-store")
        }

        next.ServeHTTP(w, r)
    })
}

func RunServer(r *mux.Router) error {
    address := getServerParameters(config.Web)
    assetsDelivery(r)
    checkCustomCss()

    if folder := config.Paste.AssetsFolder; "" != folder {
        slog.Info("assets folder takes precedence over the embedded ones", "folder", folder)
    }

    limiter, err := ratelimit.New(
        config.Web.RateLimit,
        config.Web.TrustedProxies,
        ratelimit.WithLimitHandler(http.HandlerFunc(RateLimitController)),
    )
    if nil != err {
        // Only a malformed trusted proxy CIDR gets here, which is a
        // configuration error: starting anyway would silently trust no proxy
        // and rate limit every client behind it as one.
        return err
    }

    // Security headers outermost, so a 429 carries them too; the quota is then
    // spent before the body is read.
    handler := securityHeaders(limitRequestRate(limiter, limitBodySize(r)))

    // Explicit timeouts: the zero values of ListenAndServe let a client hold a
    // connection open forever, which is all Slowloris needs.
    server := &http.Server{
        Addr:              address,
        Handler:           handler,
        ReadHeaderTimeout: 10 * time.Second,
        ReadTimeout:       30 * time.Second,
        WriteTimeout:      30 * time.Second,
        IdleTimeout:       120 * time.Second,
    }

    slog.Info("running server", "address", address, "rate_limit", config.Web.RateLimit)

    // ListenAndServe never returns a nil error. Ignoring it made the process
    // exit with a 0 status when the port was already taken.
    return server.ListenAndServe()
}
