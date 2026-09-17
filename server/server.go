package main

import (
    "crypto/sha256"
    "embed"
    "encoding/base64"
    "io/fs"
    "log/slog"
    "net"
    "net/http"
    "net/url"
    "os"
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

// staticPrefix is where the assets are mounted. The templates never spell it
// out: they name an asset by its path inside the tree and let assetUrl build
// the url, so the mount point and the fingerprint are decided in one place.
const staticPrefix = "/static"

// maxRequestBytes caps the whole request body. Pastes are posted as a comma
// separated list of decimal bytes, so roughly four characters per byte of clear
// text: this leaves about one megabyte of text. Without it ParseMultipartForm
// spills to disk past 32 MB with no global ceiling at all.
const maxRequestBytes = 4 << 20

// maxPasteContentBytes is what is left for the paste field itself once the
// other fields and the multipart envelope are accounted for. The creation page
// hands it to the browser, which checks the encrypted payload against it before
// uploading a body the server would only answer 413 to.
const maxPasteContentBytes = maxRequestBytes - 8<<10

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

// assetETags maps each embedded asset to a validator derived from its bytes.
// It is built once at startup: the embedded tree cannot change while the
// process runs, and a hash keeps the same value across restarts and across
// instances, where a modification time would differ on each of them.
//
// Without a validator of any kind, embed.FS reports a zero modification time,
// http.ServeContent omits Last-Modified, and a browser has nothing to
// revalidate against: every navigation pulled the fonts and the scripts again.
var assetETags = map[string]string{}

func computeAssetETags() {
    assets, err := fs.Sub(assetsFS, "assets")
    if nil != err {
        panic(err)
    }

    walkErr := fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, err error) error {
        if nil != err || entry.IsDir() {
            return err
        }

        content, err := fs.ReadFile(assets, path)
        if nil != err {
            return err
        }

        // Half a SHA-256 is far more than what a cache validator needs, and it
        // keeps the header short.
        sum := sha256.Sum256(content)
        assetETags["/"+path] = `"` + base64.RawURLEncoding.EncodeToString(sum[:16]) + `"`

        return nil
    })

    if nil != walkErr {
        // Serving without validators is slower, not broken: trace and carry on.
        slog.Error("fail to fingerprint the embedded assets", "err", walkErr)
    }
}

// assetVersionBytes is how much of a fingerprint travels in an url. This is not
// a cache validator, the ETag stays in charge of that: the token only has to
// change when the file does, and six bytes of SHA-256 is already far more than
// a handful of assets need.
const assetVersionBytes = 6

// assetVersions maps each asset to a short fingerprint of its content, for the
// url where assetETags is for the header. Stamping the url is what earns an
// asset a year of caching: a deployment changes the url, so the new bytes are
// picked up at once rather than waited out.
//
// The application version would have been the obvious token, but it stays
// "develop" outside a release and it does not move when the operator edits a
// file of OPEN_GO_PASTE_ASSETS_FOLDER. Both cases would pin a stale asset in
// the browsers for a year, where the content always answers for itself.
var assetVersions = map[string]string{}

// computeAssetVersions fingerprints the binary and, when there is one, the
// operator folder. An overridden asset stays revalidated whatever its url says,
// see assetCacheHeaders, but fingerprinting it means a restart is enough for
// the new bytes to reach the browsers instead of waiting the max-age out.
func computeAssetVersions() {
    assets, err := fs.Sub(assetsFS, "assets")
    if nil != err {
        panic(err)
    }

    if err := fingerprintAssets(assets); nil != err {
        // An unstamped url is simply revalidated, which is what every asset did
        // before: trace and carry on.
        slog.Error("fail to fingerprint the embedded assets", "err", err)
    }

    folder := config.Paste.AssetsFolder
    if "" == folder {
        return
    }

    if err := fingerprintAssets(os.DirFS(folder)); nil != err {
        slog.Warn("fail to fingerprint the assets folder", "folder", folder, "err", err)
    }
}

func fingerprintAssets(assets fs.FS) error {
    return fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, err error) error {
        if nil != err || entry.IsDir() {
            return err
        }

        content, err := fs.ReadFile(assets, path)
        if nil != err {
            return err
        }

        sum := sha256.Sum256(content)
        assetVersions["/"+path] = base64.RawURLEncoding.EncodeToString(sum[:assetVersionBytes])

        return nil
    })
}

// assetUrl is what the templates call instead of writing a "/static/..." path
// by hand: it stamps the url with the fingerprint of the file it points to,
// which is what assetCacheHeaders recognises before answering "immutable".
//
// An asset it does not know is returned unstamped rather than dropped: the page
// keeps working, that url is just revalidated the way it always was.
func assetUrl(path string) string {
    version, known := assetVersions[path]
    if !known {
        return staticPrefix + path
    }

    return staticPrefix + path + "?v=" + version
}

// isCurrentAssetVersion tells whether the url carries the fingerprint of what is
// about to be served. The token is compared rather than merely noticed: pinning
// a year of caching on any token at all would let a shared cache be filled with
// copies of every asset under made up urls.
func isCurrentAssetVersion(name string, r *http.Request) bool {
    version := r.URL.Query().Get("v")
    if "" == version {
        return false
    }

    expected, known := assetVersions[name]

    return known && expected == version
}

// cacheControlFor answers how long a browser may keep an asset without asking
// again. An url carrying the fingerprint of its content cannot go stale, and
// neither can a font, which carries its version in its file name: both are
// immutable. What is left is revalidated often enough for a deployment to show
// up quickly, which the ETag turns into a 304.
func cacheControlFor(path string, versioned bool) string {
    if versioned || strings.HasPrefix(path, "/fonts/") {
        return "public, max-age=31536000, immutable"
    }

    return "public, max-age=300"
}

// assetCacheHeaders states how long the asset may be kept and what identifies
// this version of it. http.ServeContent reads the ETag back out of the header
// to answer If-None-Match, so the 304 costs nothing here.
func assetCacheHeaders(overlay http.FileSystem, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        name := strings.TrimPrefix(r.URL.Path, staticPrefix)

        // The operator folder takes precedence over the binary, so the
        // fingerprint of an embedded file would be a lie for anything it
        // overrides. Those keep the modification time read from the disk, and
        // they are never made immutable: the folder is meant to be edited
        // without rebuilding, and a restart is not always what follows.
        overridden := overlayHas(overlay, name)

        header := w.Header()
        header.Set("Cache-Control", cacheControlFor(name, !overridden && isCurrentAssetVersion(name, r)))

        if etag, ok := assetETags[name]; ok && !overridden {
            header.Set("ETag", etag)
        }

        next.ServeHTTP(w, r)
    })
}

func overlayHas(overlay http.FileSystem, name string) bool {
    if nil == overlay {
        return false
    }

    file, err := overlay.Open(name)
    if nil != err {
        return false
    }

    file.Close()

    return true
}

func assetsDelivery(r *mux.Router) {
    computeAssetETags()
    computeAssetVersions()

    overlay := assetsOverlay()
    fileSystem := overlayFileSystem{
        embedded: embeddedAssets(),
        overlay:  overlay,
    }

    delivery := http.StripPrefix("/static/", http.FileServer(fileSystem))

    r.PathPrefix("/static/").Handler(assetCacheHeaders(overlay, delivery))
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
