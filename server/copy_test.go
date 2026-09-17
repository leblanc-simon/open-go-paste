package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func renderedPage(t *testing.T, controller func(http.ResponseWriter, *http.Request), path string, language string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", language)
	recorder := httptest.NewRecorder()

	controller(recorder, request)

	return recorder.Body.String()
}

// Every page used to answer "OpenGoPaste", so a failed read was
// indistinguishable from a successful one in a list of tabs, in the history, and
// to a screen reader announcing the new page.
func TestEachPageNamesItself(t *testing.T) {
	useTempPasteFolder(t)

	titles := map[string]string{
		"index": extractTitle(t, renderedPage(t, IndexController, "/", "en")),
		"about": extractTitle(t, renderedPage(t, AboutController, "/about", "en")),
		"error": extractTitle(t, renderedPage(t, NotFoundController, "/nowhere", "en")),
	}

	seen := map[string]string{}
	for page, title := range titles {
		if "" == title || "OpenGoPaste" == title {
			t.Errorf("the %s page carries the bare service name: %q", page, title)
		}

		if !strings.HasSuffix(title, "OpenGoPaste") {
			t.Errorf("the %s page drops the service name: %q", page, title)
		}

		if other, taken := seen[title]; taken {
			t.Errorf("the %s page and the %s page share the title %q", page, other, title)
		}

		seen[title] = page
	}
}

// A paste is answered with a 404 like any missing page, but the reason is not
// the same: it expired or it was read out. Saying so spares the reader a hunt
// for a mistake of their own.
func TestAMissingPasteExplainsWhyRatherThanRepeatingItsTitle(t *testing.T) {
	useTempPasteFolder(t)

	request := httptest.NewRequest(http.MethodGet, "/p/6584ae18-007a-472f-be90-1d1ec6de33d2", nil)
	request.Header.Set("User-Agent", "Mozilla/5.0 Firefox/128.0")
	request = mux.SetURLVars(request, map[string]string{"pasteId": "6584ae18-007a-472f-be90-1d1ec6de33d2"})
	recorder := httptest.NewRecorder()

	GetPasteController(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusNotFound)
	}

	body := recorder.Body.String()

	for _, expected := range []string{"expired", "destroyed"} {
		if !strings.Contains(body, expected) {
			t.Errorf("the answer never mentions %q: the reader is left guessing", expected)
		}
	}

	// The heading used to be repeated verbatim as the message underneath it.
	if heading, message := extractTitle(t, body), extractMessage(t, body); heading == message {
		t.Errorf("the heading is repeated as the message: %q", message)
	}

	// And it names what is missing, rather than borrowing the wording of a
	// wrong address.
	if !strings.Contains(body, "Paste not found") {
		t.Error("a destroyed paste is announced as a missing page")
	}
}

// The status line is for tools, not for readers: it used to open every heading.
func TestTheErrorHeadingsCarryNoStatusCode(t *testing.T) {
	useTempPasteFolder(t)

	body := renderedPage(t, NotFoundController, "/nowhere", "en")

	for _, code := range []string{"404", "400", "405", "429"} {
		if strings.Contains(body, code+" ") {
			t.Errorf("the page shows the status code %q to the reader", code)
		}
	}
}

// Zero means "no limit", which neither the label nor the field let anyone
// guess: people typed 0 believing it locked the paste down.
func TestTheReadsFieldExplainsItsZero(t *testing.T) {
	body := renderedPage(t, IndexController, "/", "en")

	if !strings.Contains(body, `aria-describedby="max_read_hint"`) {
		t.Error("the field is not tied to its hint")
	}

	if !strings.Contains(body, `id="max_read_hint"`) {
		t.Error("the hint is missing from the page")
	}

	if !strings.Contains(body, "Enter 0 to leave the number of reads unlimited.") {
		t.Error("the hint does not say what zero does")
	}
}

// The key travels inside the link: losing the link loses the paste, and nothing
// on the page used to say so.
func TestTheLinkCarriesItsWarning(t *testing.T) {
	body := renderedPage(t, IndexController, "/", "en")

	if !strings.Contains(body, "The key that decrypts this paste is part of the link") {
		t.Error("nothing warns that the key lives in the link")
	}
}

// The form fields are named max_read and max_time in the code; a reader has no
// way of knowing that, and the validation messages used to quote them.
func TestTheValidationMessagesNameNoInternalField(t *testing.T) {
	useTempPasteFolder(t)

	for _, field := range []string{"max_read", "max_time"} {
		fields := validSubmission()
		fields[field] = "obviously wrong"

		recorder := httptest.NewRecorder()
		PostPasteController(recorder, newSubmission(t, fields))

		body := recorder.Body.String()
		// The hint of the creation page legitimately carries the id, the error
		// page has no such excuse.
		if strings.Contains(body, field) {
			t.Errorf("the answer for %s quotes the internal field name: %q", field, body)
		}
	}
}

func extractMessage(t *testing.T, body string) string {
	t.Helper()

	const opening = `<div class="message">`

	start := strings.Index(body, opening)
	if start < 0 {
		t.Fatal("the page carries no message")
	}

	rest := body[start+len(opening):]

	return strings.TrimSpace(rest[:strings.Index(rest, "</div>")])
}

func extractTitle(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, "<title>")
	end := strings.Index(body, "</title>")
	if start < 0 || end < start {
		t.Fatal("the page carries no title")
	}

	return body[start+len("<title>") : end]
}
