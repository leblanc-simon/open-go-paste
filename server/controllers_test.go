package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"leblanc.io/open-go-base/i18n"
)

func init() {
	InitLocales()
	InitTemplateViews()

	// RunServer fingerprints the assets before anything can be rendered; the
	// tests reach the controllers directly, so they have to do it themselves or
	// every page would come out with unstamped urls.
	computeAssetVersions()
}

func newLocaleForTest(t *testing.T, acceptLanguage string) *i18n.Localizer {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Language", acceptLanguage)

	return NewLocale(request)
}

func TestLocaleLangMatchesAcceptLanguage(t *testing.T) {
	// The catalogues only cover "en" and "fr", but the lang attribute carries
	// the complete BCP 47 tag actually negotiated, region extension included: a
	// fr-CA reader is served the French catalogue and formats its dates the
	// Canadian way, and "fr-u-rg-cazzzz" is what says so. A language matching
	// nothing falls back to the plain default tag.
	cases := map[string]string{
		"fr":             "fr",
		"fr-FR,fr;q=0.9": "fr-u-rg-frzzzz",
		"fr-CA":          "fr-u-rg-cazzzz",
		"en":             "en",
		"en-US,en;q=0.9": "en-u-rg-uszzzz",
		"":               "en",
		"zz":             "en",
		"de-DE":          "en",
	}

	for acceptLanguage, expected := range cases {
		locale := newLocaleForTest(t, acceptLanguage)
		if locale.Lang().String() != expected {
			t.Errorf("Accept-Language %q: got lang %q, want %q", acceptLanguage, locale.Lang().String(), expected)
		}
	}
}

// TestIndexControllerIsLanguageIsolated is the regression test for the data race
// on the former package level "locale" variable: concurrent requests used to
// overwrite each other's language. Run it with -race.
func TestIndexControllerIsLanguageIsolated(t *testing.T) {
	cases := []struct {
		acceptLanguage string
		expectedLang   string
		expectedLabel  string
	}{
		{"fr", "fr", "Expire dans"},
		{"en-US", "en-u-rg-uszzzz", "Expires in"},
	}

	var waitGroup sync.WaitGroup

	for i := 0; i < 50; i++ {
		for _, testCase := range cases {
			waitGroup.Add(1)

			go func(acceptLanguage string, expectedLang string, expectedLabel string) {
				defer waitGroup.Done()

				request := httptest.NewRequest(http.MethodGet, "/", nil)
				request.Header.Set("Accept-Language", acceptLanguage)
				recorder := httptest.NewRecorder()

				IndexController(recorder, request)

				if recorder.Code != http.StatusOK {
					t.Errorf("Accept-Language %q: got status %d, want %d", acceptLanguage, recorder.Code, http.StatusOK)
					return
				}

				body := recorder.Body.String()

				if !strings.Contains(body, `<html lang="`+expectedLang+`"`) {
					t.Errorf("Accept-Language %q: lang attribute is not %q", acceptLanguage, expectedLang)
				}

				if !strings.Contains(body, expectedLabel) {
					t.Errorf("Accept-Language %q: body does not contain %q", acceptLanguage, expectedLabel)
				}
			}(testCase.acceptLanguage, testCase.expectedLang, testCase.expectedLabel)
		}
	}

	waitGroup.Wait()
}

func TestErrorHandlerAlwaysHasATitle(t *testing.T) {
	locale := newLocaleForTest(t, "en")

	for _, code := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusTooManyRequests, http.StatusInternalServerError} {
		if title := errorTitle(locale, code); title == "" {
			t.Errorf("status %d: empty error title", code)
		}
	}
}

func TestErrorHandlerRendersTheStatusCode(t *testing.T) {
	recorder := httptest.NewRecorder()

	ErrorHandler(recorder, newLocaleForTest(t, "en"), "boom", http.StatusBadRequest)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	if !strings.Contains(recorder.Body.String(), "boom") {
		t.Error("the error message is missing from the body")
	}
}

// The paste view hands its data over through data-* attributes: an inline script
// would need a Content-Security-Policy with 'unsafe-inline'.
func TestGetPasteControllerRendersTheDataAttributes(t *testing.T) {
	useTempPasteFolder(t)

	pasteId, err := CreatePaste("1,2,3", "markdown", "1h", 2)
	if nil != err {
		t.Fatalf("fail to create the paste: %s", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/p/"+pasteId, nil)
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/128.0")
	request = mux.SetURLVars(request, map[string]string{"pasteId": pasteId})
	recorder := httptest.NewRecorder()

	GetPasteController(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()

	for _, attribute := range []string{
		`data-encrypted-value="1,2,3"`,
		`data-nb-of-read-allowed="2"`,
		`data-nb-of-read="1"`,
		`data-type="markdown"`,
	} {
		if !strings.Contains(body, attribute) {
			t.Errorf("the body does not contain %s", attribute)
		}
	}

	// The expiration date must be parsable by the browser as is.
	expireTime := extractAttribute(t, body, "data-expire-time")
	if _, err := time.Parse(time.RFC3339, expireTime); nil != err {
		t.Errorf("data-expire-time %q is not RFC 3339: %s", expireTime, err)
	}

	if strings.Contains(body, "const encryptedValue") {
		t.Error("the inline script is still rendered")
	}
}

func extractAttribute(t *testing.T, body string, attribute string) string {
	t.Helper()

	start := strings.Index(body, attribute+`="`)
	if start < 0 {
		t.Fatalf("attribute %s is missing from the body", attribute)
	}

	start += len(attribute) + 2
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("attribute %s is not terminated", attribute)
	}

	return body[start : start+end]
}

func TestIsValidUserAgent(t *testing.T) {
	cases := map[string]bool{
		"Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/128.0":             true,
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)": false,
		"Mozilla/5.0 (compatible; SkypeUriPreview Preview/0.5)":                    false,
		"curl/8.5.0": false,
		"":           false,
	}

	for userAgent, expected := range cases {
		request := httptest.NewRequest(http.MethodGet, "/p/id", nil)
		request.Header.Set("User-Agent", userAgent)

		if got := IsValidUserAgent(request); got != expected {
			t.Errorf("user agent %q: got %t, want %t", userAgent, got, expected)
		}
	}
}
