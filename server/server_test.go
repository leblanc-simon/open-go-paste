package main

import (
    "io"
    "io/fs"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/gorilla/mux"
)

func TestSecurityHeadersAreSetOnPages(t *testing.T) {
    handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/p/id", nil))

    expected := map[string]string{
        "X-Content-Type-Options": "nosniff",
        "X-Frame-Options":        "DENY",
        "Referrer-Policy":        "no-referrer",
        "Cache-Control":          "no-store",
    }

    for header, value := range expected {
        if got := recorder.Header().Get(header); got != value {
            t.Errorf("header %s: got %q, want %q", header, got, value)
        }
    }

    csp := recorder.Header().Get("Content-Security-Policy")

    if !strings.Contains(csp, "script-src 'self'") {
        t.Errorf("the policy does not restrict the scripts: %q", csp)
    }

    // 'unsafe-inline' would void the mitigation of an injection in the rendered
    // markdown.
    if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
        t.Errorf("the policy allows inline scripts: %q", csp)
    }
}

// Static assets are the only responses allowed to be cached.
func TestStaticAssetsAreCacheable(t *testing.T) {
    handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil))

    if got := recorder.Header().Get("Cache-Control"); got != "" {
        t.Errorf("got Cache-Control %q on a static asset, want none", got)
    }
}

func TestStyleSourcesAllowsTheCustomStylesheetOrigin(t *testing.T) {
    if sources := styleSources(); sources != "'self'" {
        t.Errorf("got %q, want only 'self' without a custom stylesheet", sources)
    }

    usePasteConfig(t, PasteConfig{CustomCss: "/static/css/mine.css"})
    if sources := styleSources(); sources != "'self'" {
        t.Errorf("got %q, want only 'self' for a relative stylesheet", sources)
    }

    usePasteConfig(t, PasteConfig{CustomCss: "https://cdn.example.org/theme/mine.css"})
    if sources := styleSources(); sources != "'self' https://cdn.example.org" {
        t.Errorf("got %q, want the stylesheet origin", sources)
    }
}

// Everything the pages ask for has to be in the binary, otherwise the service
// serves a broken page from a directory it no longer needs.
func TestTheEmbeddedAssetsHoldEverythingThePagesAskFor(t *testing.T) {
    assets := embeddedAssets()

    for _, name := range []string{
        "/css/app.css",
        "/css/color.css",
        "/css/highlightjs/androidstudio.css",
        "/js/app.js",
        "/js/crypto.js",
        "/js/sanitize.js",
        "/js/highlight.min.js",
        "/js/showdown.min.js",
        "/img/logo3.svg",
        "/img/favicon/favicon.ico",
    } {
        file, err := assets.Open(name)
        if nil != err {
            t.Errorf("%s is missing from the binary: %s", name, err)
            continue
        }

        file.Close()
    }
}

// The embed directive lists the stylesheets one by one, so a new asset can be
// forgotten. This walks the folder on disk and holds the two in sync, with
// custom.css as the single intended exception.
func TestTheEmbeddedAssetsMatchTheAssetsFolder(t *testing.T) {
    err := filepath.WalkDir("assets", func(path string, entry fs.DirEntry, err error) error {
        if nil != err {
            return err
        }

        if entry.IsDir() {
            return nil
        }

        name := filepath.ToSlash(path)
        _, statError := fs.Stat(assetsFS, name)

        if customCssName == name {
            // Git ignored, written by the operator: it must never travel in a
            // release binary.
            if nil == statError {
                t.Errorf("%s is embedded in the binary", name)
            }

            return nil
        }

        if nil != statError {
            t.Errorf("%s is on disk but not in the binary: add it to the embed directive", name)
        }

        return nil
    })

    if nil != err {
        t.Fatalf("fail to walk the assets folder: %s", err)
    }
}

func TestTheTemplatesAreEmbedded(t *testing.T) {
    layouts := layoutFiles()
    if len(layouts) == 0 {
        t.Fatal("no layout found in the binary")
    }

    // NewView panics on a missing template, and the views are built by the
    // package init of the tests: reaching this point already proves the parsing
    // worked from the embedded filesystem.
    if nil == pasteView || nil == indexView {
        t.Error("the views have not been built")
    }
}

// The operator stylesheet is written at deployment time: it cannot live in the
// binary, so a folder on disk has to win over the embedded assets.
func TestTheAssetsFolderOverridesTheEmbeddedOnes(t *testing.T) {
    folder := t.TempDir()

    if err := os.MkdirAll(filepath.Join(folder, "css"), 0700); nil != err {
        t.Fatalf("fail to create the css directory: %s", err)
    }

    custom := []byte("body { color: red; }")
    if err := os.WriteFile(filepath.Join(folder, "css", "custom.css"), custom, 0600); nil != err {
        t.Fatalf("fail to write the custom stylesheet: %s", err)
    }

    // An asset that also exists in the binary, to check which one wins.
    override := []byte("/* overridden */")
    if err := os.WriteFile(filepath.Join(folder, "css", "app.css"), override, 0600); nil != err {
        t.Fatalf("fail to write the overriding stylesheet: %s", err)
    }

    usePasteConfig(t, PasteConfig{AssetsFolder: folder})

    router := mux.NewRouter()
    assetsDelivery(router)

    cases := map[string]string{
        "/static/css/custom.css": string(custom),
        "/static/css/app.css":    string(override),
    }

    for path, expected := range cases {
        recorder := httptest.NewRecorder()
        router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

        if recorder.Code != http.StatusOK {
            t.Errorf("%s: got status %d, want %d", path, recorder.Code, http.StatusOK)
            continue
        }

        if recorder.Body.String() != expected {
            t.Errorf("%s: got %q, want %q", path, recorder.Body.String(), expected)
        }
    }

    // An asset absent from the folder still comes from the binary.
    recorder := httptest.NewRecorder()
    router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil))

    if recorder.Code != http.StatusOK {
        t.Errorf("got status %d for an embedded only asset, want %d", recorder.Code, http.StatusOK)
    }
}

func TestTheEmbeddedAssetsAreServedWithoutAnyFolder(t *testing.T) {
    router := mux.NewRouter()
    assetsDelivery(router)

    recorder := httptest.NewRecorder()
    router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/static/js/crypto.js", nil))

    if recorder.Code != http.StatusOK {
        t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
    }

    if !strings.Contains(recorder.Body.String(), "AES-GCM") {
        t.Error("the served script is not the one from the binary")
    }
}

func TestLimitBodySizeRejectsAnOversizedBody(t *testing.T) {
    var readError error

    handler := limitBodySize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _, readError = io.Copy(io.Discard, r.Body)
    }))

    body := strings.NewReader(strings.Repeat("1,", maxRequestBytes))
    request := httptest.NewRequest(http.MethodPost, "/", body)

    handler.ServeHTTP(httptest.NewRecorder(), request)

    if nil == readError {
        t.Error("a body twice the allowed size has been read entirely")
    }
}

func TestLimitBodySizeLeavesAnAcceptableBodyAlone(t *testing.T) {
    var readBytes int64
    var readError error

    handler := limitBodySize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        readBytes, readError = io.Copy(io.Discard, r.Body)
    }))

    body := strings.NewReader(strings.Repeat("a", 1024))
    request := httptest.NewRequest(http.MethodPost, "/", body)

    handler.ServeHTTP(httptest.NewRecorder(), request)

    if nil != readError {
        t.Errorf("unexpected error on a small body: %s", readError)
    }

    if readBytes != 1024 {
        t.Errorf("got %d bytes read, want 1024", readBytes)
    }
}
