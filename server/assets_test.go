package main

import (
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/gorilla/mux"
)

// newAssetServer wires the static delivery the way RunServer does.
func newAssetServer(t *testing.T) http.Handler {
    t.Helper()

    router := mux.NewRouter()
    assetsDelivery(router)

    return router
}

func getAsset(t *testing.T, handler http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
    t.Helper()

    request := httptest.NewRequest(http.MethodGet, path, nil)
    for name, value := range headers {
        request.Header.Set(name, value)
    }

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)

    return recorder
}

// embed.FS reports a zero modification time, so http.ServeContent used to send
// neither Last-Modified nor ETag: a browser had nothing to revalidate against
// and pulled the fonts and the scripts again on every navigation.
func TestStaticAssetsCarryAValidator(t *testing.T) {
    handler := newAssetServer(t)

    for _, path := range []string{
        "/static/js/app.js",
        "/static/css/app.css",
        "/static/js/highlight.min.js",
        "/static/img/logo3.svg",
    } {
        recorder := getAsset(t, handler, path, nil)

        if recorder.Code != http.StatusOK {
            t.Fatalf("%s: got status %d, want %d", path, recorder.Code, http.StatusOK)
        }

        if "" == recorder.Header().Get("ETag") {
            t.Errorf("%s carries no ETag", path)
        }

        if "" == recorder.Header().Get("Cache-Control") {
            t.Errorf("%s carries no Cache-Control", path)
        }
    }
}

func TestAKnownAssetIsAnsweredWithNotModified(t *testing.T) {
    handler := newAssetServer(t)

    first := getAsset(t, handler, "/static/js/highlight.min.js", nil)
    etag := first.Header().Get("ETag")

    if first.Body.Len() < 1000 {
        t.Fatalf("the first answer carries %d bytes, expected the whole bundle", first.Body.Len())
    }

    second := getAsset(t, handler, "/static/js/highlight.min.js", map[string]string{
        "If-None-Match": etag,
    })

    if second.Code != http.StatusNotModified {
        t.Fatalf("got status %d, want %d", second.Code, http.StatusNotModified)
    }

    if 0 != second.Body.Len() {
        t.Errorf("the revalidation carries %d bytes, want none", second.Body.Len())
    }
}

// The validator comes from the bytes, not from the clock: it has to survive a
// restart, and match across instances.
func TestTheValidatorIsDerivedFromTheContent(t *testing.T) {
    handler := newAssetServer(t)
    first := getAsset(t, handler, "/static/css/app.css", nil).Header().Get("ETag")

    // A second startup, as a redeploy or another instance would do.
    assetETags = map[string]string{}
    second := getAsset(t, newAssetServer(t), "/static/css/app.css", nil).Header().Get("ETag")

    if first != second {
        t.Errorf("the validator changed across startups: %q then %q", first, second)
    }

    other := getAsset(t, handler, "/static/js/app.js", nil).Header().Get("ETag")
    if first == other {
        t.Error("two different assets share a validator")
    }
}

// The fonts carry their version in their file name, so they never change under
// it; the rest has to be revalidated often enough for a deployment to show up.
func TestFontsAreCachedLongerThanTheRest(t *testing.T) {
    handler := newAssetServer(t)

    font := getAsset(t, handler, "/static/fonts/titillium-web-v7-latin_latin-ext-700.woff2", nil)
    if !strings.Contains(font.Header().Get("Cache-Control"), "immutable") {
        t.Errorf("got Cache-Control %q on a font, want it immutable", font.Header().Get("Cache-Control"))
    }

    script := getAsset(t, handler, "/static/js/app.js", nil)
    if strings.Contains(script.Header().Get("Cache-Control"), "immutable") {
        t.Errorf("got Cache-Control %q on a script, want it revalidated", script.Header().Get("Cache-Control"))
    }
}

// An operator folder takes precedence over the binary, so the fingerprint of
// the embedded file would be a lie for anything it overrides.
func TestAnOverriddenAssetDoesNotKeepTheEmbeddedValidator(t *testing.T) {
    folder := t.TempDir()
    if err := os.MkdirAll(filepath.Join(folder, "css"), 0o755); nil != err {
        t.Fatalf("fail to prepare the overlay: %s", err)
    }

    override := filepath.Join(folder, "css", "app.css")
    if err := os.WriteFile(override, []byte("body { color: red }"), 0o644); nil != err {
        t.Fatalf("fail to write the override: %s", err)
    }

    usePasteConfig(t, PasteConfig{AssetsFolder: folder})

    handler := newAssetServer(t)
    recorder := getAsset(t, handler, "/static/css/app.css", nil)

    if !strings.Contains(recorder.Body.String(), "color: red") {
        t.Fatal("the overlay file was not served")
    }

    if "" != recorder.Header().Get("ETag") {
        t.Errorf("the overridden asset carries the embedded validator %q", recorder.Header().Get("ETag"))
    }
}

// app.js imports crypto.js and sanitize.js: without the hint the browser only
// discovers them after parsing app.js, one round trip in series.
func TestThePagesPreloadTheModulesTheyImport(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/", nil)
    recorder := httptest.NewRecorder()
    IndexController(recorder, request)

    body := recorder.Body.String()

    for _, module := range []string{"crypto.js", "sanitize.js"} {
        hint := `<link rel="modulepreload" href="/static/js/` + module + `">`
        if !strings.Contains(body, hint) {
            t.Errorf("the page does not preload %s", module)
        }
    }
}
