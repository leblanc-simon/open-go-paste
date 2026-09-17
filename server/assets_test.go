package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

// A url carrying the fingerprint of its content cannot go stale, which is the
// whole point of stamping it: the browser stops revalidating a dozen assets on
// every navigation, and a deployment still shows up at once under a new url.
func TestAStampedAssetIsCachedForAYear(t *testing.T) {
	handler := newAssetServer(t)

	plain := getAsset(t, handler, "/static/css/app.css", nil)
	if strings.Contains(plain.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("got Cache-Control %q on an unstamped url, want it revalidated", plain.Header().Get("Cache-Control"))
	}

	stamped := getAsset(t, handler, assetUrl("/css/app.css"), nil)
	if stamped.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", stamped.Code, http.StatusOK)
	}

	cacheControl := stamped.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "immutable") {
		t.Errorf("got Cache-Control %q on a stamped url, want it immutable", cacheControl)
	}

	// The validator is still there: a browser that drops the entry early, or a
	// proxy that ignores immutable, revalidates rather than downloads again.
	if "" == stamped.Header().Get("ETag") {
		t.Error("the stamped asset carries no ETag")
	}
}

// The token is compared, not merely noticed. Pinning a year of caching on any
// token at all would let a shared cache be filled with copies of every asset
// under made up urls.
func TestAStampThatIsNotTheCurrentOneIsRevalidated(t *testing.T) {
	handler := newAssetServer(t)

	for _, path := range []string{
		"/static/css/app.css?v=deadbeef",
		"/static/css/app.css?v=",
		"/static/css/app.css?v=" + assetVersions["/js/app.js"],
	} {
		recorder := getAsset(t, handler, path, nil)

		if strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s: got Cache-Control %q, want it revalidated", path, recorder.Header().Get("Cache-Control"))
		}
	}
}

// The stamp comes from the bytes rather than from the release: it has to hold
// across instances, and to move for the one file that changed, not for all of
// them.
func TestTheStampIsDerivedFromTheContent(t *testing.T) {
	newAssetServer(t)

	stylesheet, script := assetVersions["/css/app.css"], assetVersions["/js/app.js"]

	if "" == stylesheet || "" == script {
		t.Fatal("the assets have not been fingerprinted")
	}

	if stylesheet == script {
		t.Error("two different assets share a stamp")
	}

	// A second startup, as a redeploy or another instance would do.
	assetVersions = map[string]string{}
	newAssetServer(t)

	if assetVersions["/css/app.css"] != stylesheet {
		t.Errorf("the stamp changed across startups: %q then %q", stylesheet, assetVersions["/css/app.css"])
	}
}

// The operator folder is meant to be edited without rebuilding, and a restart
// is not always what follows: an overridden asset has to keep being
// revalidated, whatever its url claims.
func TestAnOverriddenAssetIsNeverImmutable(t *testing.T) {
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
	recorder := getAsset(t, handler, assetUrl("/css/app.css"), nil)

	if !strings.Contains(recorder.Body.String(), "color: red") {
		t.Fatal("the overlay file was not served")
	}

	if strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("got Cache-Control %q on an overridden asset, want it revalidated", recorder.Header().Get("Cache-Control"))
	}

	// Stamping it is still worth it: the url moves on the next restart, so the
	// new bytes reach the browsers then rather than once the max-age is out.
	if !strings.Contains(assetUrl("/css/app.css"), "?v=") {
		t.Error("the overridden asset carries no stamp at all")
	}
}

// An unstamped url is served the way it always was, so a page never breaks on
// an asset the fingerprinting missed.
func TestAnUnknownAssetIsLinkedWithoutAStamp(t *testing.T) {
	newAssetServer(t)

	if url := assetUrl("/css/nothing-like-this.css"); url != "/static/css/nothing-like-this.css" {
		t.Errorf("got %q, want the plain path", url)
	}
}

// The stamps are useless if the pages do not carry them.
func TestThePagesLinkTheirAssetsWithAStamp(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	IndexController(recorder, request)

	body := recorder.Body.String()

	for _, asset := range []string{"/css/app.css", "/css/color.css", "/js/app.js", "/img/logo3.svg"} {
		if !strings.Contains(body, assetUrl(asset)) {
			t.Errorf("the page does not link %s with its stamp", asset)
		}
	}
}

// Every asset url is built by the template helper, so that it carries the stamp
// that earns it a year of caching. The two module hints are the deliberate
// exception: app.js imports crypto.js and sanitize.js relatively, and a module
// url carries no query along, so a stamped hint would preload a url the import
// never asks for, and the file would travel twice.
func TestTheModuleHintsMatchWhatTheScriptImports(t *testing.T) {
	script, err := fs.ReadFile(assetsFS, "assets/js/app.js")
	if nil != err {
		t.Fatalf("fail to read app.js: %s", err)
	}

	unstamped := map[string]bool{}
	for _, imported := range regexp.MustCompile(`from '\./([^']+)'`).FindAllStringSubmatch(string(script), -1) {
		unstamped["/static/js/"+imported[1]] = true
	}

	if 0 == len(unstamped) {
		t.Fatal("app.js imports nothing, the hints have nothing to match")
	}

	written := regexp.MustCompile(`/static/[A-Za-z0-9_./-]+`)

	err = fs.WalkDir(templatesFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if nil != err || entry.IsDir() {
			return err
		}

		content, err := fs.ReadFile(templatesFS, path)
		if nil != err {
			return err
		}

		for _, url := range written.FindAllString(string(content), -1) {
			if !unstamped[url] {
				t.Errorf("%s writes %s by hand: it has to go through the asset helper", path, url)
			}
		}

		return nil
	})

	if nil != err {
		t.Fatalf("fail to walk the templates: %s", err)
	}
}
