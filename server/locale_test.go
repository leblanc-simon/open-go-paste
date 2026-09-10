package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func renderAboutForTest(t *testing.T, acceptLanguage string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/about", nil)
	request.Header.Set("Accept-Language", acceptLanguage)

	recorder := httptest.NewRecorder()
	AboutController(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
	}

	return recorder
}

// templateKeyPattern matches the {{ T "key" }} and {{ TH "key" }} calls of the
// views, goKeyPattern the locale.T("key") calls of the controllers.
var (
	templateKeyPattern = regexp.MustCompile(`\{\{-?\s*TH?\s+"([^"]+)"`)
	goKeyPattern       = regexp.MustCompile(`locale\.T\("([^"]+)"\)`)
)

// translationKeysUsed collects every message id the service asks for. The keys
// built from a list, the durations and the paste types, are added from that same
// list rather than matched: no pattern can see through a concatenation.
func translationKeysUsed(t *testing.T) []string {
	t.Helper()

	keys := map[string]struct{}{}

	err := fs.WalkDir(templatesFS, "templates", func(path string, entry fs.DirEntry, err error) error {
		if nil != err {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		content, err := fs.ReadFile(templatesFS, path)
		if nil != err {
			return err
		}

		for _, match := range templateKeyPattern.FindAllStringSubmatch(string(content), -1) {
			keys[match[1]] = struct{}{}
		}

		return nil
	})

	if nil != err {
		t.Fatalf("fail to walk the templates: %s", err)
	}

	sources, err := os.ReadDir(".")
	if nil != err {
		t.Fatalf("fail to list the sources: %s", err)
	}

	for _, entry := range sources {
		// The test files are skipped: they mention keys in comments and in the
		// patterns above, none of which the service ever asks for.
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		content, err := os.ReadFile(entry.Name())
		if nil != err {
			t.Fatalf("fail to read %s: %s", entry.Name(), err)
		}

		for _, match := range goKeyPattern.FindAllStringSubmatch(string(content), -1) {
			keys[match[1]] = struct{}{}
		}
	}

	for _, duration := range allowedDurations {
		keys["duration."+duration] = struct{}{}
	}

	for _, pasteType := range allowedPasteTypes {
		keys["paste_type."+pasteType] = struct{}{}
	}

	collected := make([]string, 0, len(keys))
	for key := range keys {
		collected = append(collected, key)
	}

	sort.Strings(collected)

	return collected
}

// A missing translation renders as the message id itself, so a typo in a key or
// a message left out of one catalogue is invisible on the page. This asks every
// language for every key the service uses.
func TestEveryTranslationKeyResolvesInEveryLanguage(t *testing.T) {
	keys := translationKeysUsed(t)

	if len(keys) < 30 {
		t.Fatalf("only %d keys collected, the patterns no longer match the code", len(keys))
	}

	for _, language := range bundle.Languages() {
		locale := bundle.Localizer(language.String(), "")

		for _, key := range keys {
			if translated := locale.T(key); translated == key {
				t.Errorf("%s: %q has no translation", language, key)
			}
		}
	}
}

// The markup carrying messages are the reason TH exists: their links sit inside
// the sentence. Rendered through T they would reach the page as escaped text.
func TestTheMarkupCarryingMessagesReachThePageAsMarkup(t *testing.T) {
	recorder := renderAboutForTest(t, "en")
	body := recorder.Body.String()

	for _, expected := range []string{
		`<a href="http://www.wtfpl.net/">WTFPL</a>`,
		`<a href="http://artlibre.org/">Art Libre</a>`,
		`<a href="http://hpfteam.free.fr/graphisme-libre/">harrypopof</a>`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("the about page does not hold %s", expected)
		}
	}

	if strings.Contains(body, "&lt;a href") {
		t.Error("a markup carrying message has been escaped: it is rendered through T instead of TH")
	}
}

// app.js substitutes the §...§ markers once the paste is decrypted, matching them
// literally. An English message used to read §nBAvailableRead§ where the script
// replaces §nbAvailableRead§, so the read count stayed on the page as is.
func TestTheSubstitutionMarkersMatchTheScript(t *testing.T) {
	script, err := os.ReadFile("assets/js/app.js")
	if nil != err {
		t.Fatalf("fail to read app.js: %s", err)
	}

	markerPattern := regexp.MustCompile(`§[^§]+§`)

	known := map[string]struct{}{}
	for _, marker := range markerPattern.FindAllString(string(script), -1) {
		known[marker] = struct{}{}
	}

	if len(known) == 0 {
		t.Fatal("no substitution marker found in app.js")
	}

	for _, language := range bundle.Languages() {
		locale := bundle.Localizer(language.String(), "")

		for _, key := range []string{"paste.reads_and_expiry", "paste.expiry_only", "paste.last_read"} {
			for _, marker := range markerPattern.FindAllString(locale.T(key), -1) {
				if _, isKnown := known[marker]; !isKnown {
					t.Errorf("%s: %q holds %s, which app.js does not substitute", language, key, marker)
				}
			}
		}
	}
}
