package main

import (
    "embed"
    "io/fs"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"

    "github.com/gorilla/mux"
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

func getServerParameters() string {
    server := os.Getenv("OPEN_GO_PASTE_SERVER")
    if "" == server {
        server = "127.0.0.1"
    }

    port := os.Getenv("OPEN_GO_PASTE_PORT")
    if "" == port {
        port = "8080"
    }

    return net.JoinHostPort(server, port)
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
    folder := os.Getenv("OPEN_GO_PASTE_ASSETS_FOLDER")
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
    customCss := os.Getenv("OPEN_GO_PASTE_CUSTOM_CSS")
    if "" == customCss || !strings.HasPrefix(customCss, "/static/") {
        return
    }

    name := strings.TrimPrefix(customCss, "/static/")

    file, err := overlayFileSystem{
        embedded: embeddedAssets(),
        overlay:  assetsOverlay(),
    }.Open("/" + name)

    if nil != err {
        log.Printf(
            "Custom CSS %s is served by no asset: put the file in OPEN_GO_PASTE_ASSETS_FOLDER, or use an absolute url",
            customCss,
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

// styleSources allows the operator provided stylesheet when it is hosted
// elsewhere, without opening style-src any wider.
func styleSources() string {
    sources := "'self'"

    customCss := os.Getenv("OPEN_GO_PASTE_CUSTOM_CSS")
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
    address := getServerParameters()
    assetsDelivery(r)
    checkCustomCss()

    if folder := os.Getenv("OPEN_GO_PASTE_ASSETS_FOLDER"); "" != folder {
        log.Printf("Assets in %s take precedence over the embedded ones", folder)
    }

    // Explicit timeouts: the zero values of ListenAndServe let a client hold a
    // connection open forever, which is all Slowloris needs.
    server := &http.Server{
        Addr:              address,
        Handler:           securityHeaders(limitBodySize(r)),
        ReadHeaderTimeout: 10 * time.Second,
        ReadTimeout:       30 * time.Second,
        WriteTimeout:      30 * time.Second,
        IdleTimeout:       120 * time.Second,
    }

    log.Printf("Running server %s", address)

    // ListenAndServe never returns a nil error. Ignoring it made the process
    // exit with a 0 status when the port was already taken.
    return server.ListenAndServe()
}
