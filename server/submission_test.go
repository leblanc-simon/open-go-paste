package main

import (
    "bytes"
    "mime/multipart"
    "net/http"
    "net/http/httptest"
    "os"
    "strconv"
    "strings"
    "testing"

    "golang.org/x/text/language"
)

// newSubmission builds the multipart body a browser sends, so that the tests
// exercise the same parsing path as FormData does.
func newSubmission(t *testing.T, fields map[string]string) *http.Request {
    t.Helper()

    var body bytes.Buffer
    writer := multipart.NewWriter(&body)

    for name, value := range fields {
        if err := writer.WriteField(name, value); nil != err {
            t.Fatalf("fail to write the field %q: %s", name, err)
        }
    }

    if err := writer.Close(); nil != err {
        t.Fatalf("fail to close the multipart writer: %s", err)
    }

    request := httptest.NewRequest(http.MethodPost, "/", &body)
    request.Header.Set("Content-Type", writer.FormDataContentType())

    return request
}

func validSubmission() map[string]string {
    return map[string]string{
        "paste":     "1,2,3",
        "type":      "text",
        "max_time":  "1h",
        "max_read":  "1",
        "encrypted": "1",
    }
}

func TestPostPasteControllerStoresAnEncryptedSubmission(t *testing.T) {
    useTempPasteFolder(t)

    recorder := httptest.NewRecorder()
    PostPasteController(recorder, newSubmission(t, validSubmission()))

    if recorder.Code != http.StatusOK {
        t.Fatalf("got status %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
    }

    if !isValidUUID(strings.TrimSpace(recorder.Body.String())) {
        t.Errorf("the body does not carry a paste id: %q", recorder.Body.String())
    }
}

// The marker app.js adds is what tells an encrypted submission from one that
// went around it. Without it the service would store clear text, which is the
// one thing it promises never to hold.
func TestPostPasteControllerRejectsAnUnmarkedSubmission(t *testing.T) {
    folder := useTempPasteFolder(t)

    fields := validSubmission()
    delete(fields, "encrypted")

    recorder := httptest.NewRecorder()
    PostPasteController(recorder, newSubmission(t, fields))

    if recorder.Code != http.StatusBadRequest {
        t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
    }

    // And the message names the cause, rather than blaming the content.
    body := recorder.Body.String()
    if !strings.Contains(body, "JavaScript") {
        t.Errorf("the answer does not mention JavaScript: %q", body)
    }

    assertNoPasteStored(t, folder)
}

// The regression this guards: a phone number typed into a form submitted
// without JavaScript matches ^[0-9,]+$, so it used to sail past the content
// pattern and get stored in the clear.
func TestPostPasteControllerRejectsClearNumericContent(t *testing.T) {
    folder := useTempPasteFolder(t)

    fields := validSubmission()
    fields["paste"] = "06,12,34,56,78"
    delete(fields, "encrypted")

    recorder := httptest.NewRecorder()
    PostPasteController(recorder, newSubmission(t, fields))

    if recorder.Code != http.StatusBadRequest {
        t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
    }

    assertNoPasteStored(t, folder)
}

// An oversized body used to reach the controller as an empty paste field and be
// answered "invalid paste content", which sent the visitor looking for a problem
// in their text rather than at its length.
func TestPostPasteControllerAnswersTooLargeOnAnOversizedBody(t *testing.T) {
    useTempPasteFolder(t)

    fields := validSubmission()
    fields["paste"] = strings.TrimSuffix(strings.Repeat("1,", maxRequestBytes/2+1024), ",")

    request := newSubmission(t, fields)
    recorder := httptest.NewRecorder()
    request.Body = http.MaxBytesReader(recorder, request.Body, maxRequestBytes)

    PostPasteController(recorder, request)

    if recorder.Code != http.StatusRequestEntityTooLarge {
        t.Fatalf("got status %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
    }
}

// The creation page hands the ceiling to the browser so it can refuse a paste
// before uploading it.
func TestIndexControllerRendersTheContentCeiling(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/", nil)
    recorder := httptest.NewRecorder()

    IndexController(recorder, request)

    expected := `data-max-content-bytes="` + strconv.Itoa(maxPasteContentBytes) + `"`
    if !strings.Contains(recorder.Body.String(), expected) {
        t.Errorf("the body does not carry %s", expected)
    }
}

// The form ships locked so that a browser without JavaScript cannot submit the
// text unencrypted, and says why.
func TestIndexControllerShipsALockedForm(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/", nil)
    recorder := httptest.NewRecorder()

    IndexController(recorder, request)

    body := recorder.Body.String()

    if !strings.Contains(body, `<fieldset id="paste-fields" disabled>`) {
        t.Error("the fieldset is not disabled")
    }

    if !strings.Contains(body, "<noscript>") {
        t.Error("the page carries no noscript notice")
    }
}

// about and the error page work without JavaScript: telling their readers that
// a form is locked would be a lie, and the notice used to sit in the layout.
func TestTheNoscriptNoticeOnlyReachesThePagesThatNeedIt(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/about", nil)
    recorder := httptest.NewRecorder()
    AboutController(recorder, request)

    if strings.Contains(recorder.Body.String(), "<noscript>") {
        t.Error("the about page carries a noscript notice it has no use for")
    }

    request = httptest.NewRequest(http.MethodGet, "/", nil)
    recorder = httptest.NewRecorder()
    IndexController(recorder, request)

    if !strings.Contains(recorder.Body.String(), "<noscript>") {
        t.Error("the creation page carries no noscript notice")
    }
}

func TestTextDirectionFollowsTheScript(t *testing.T) {
    cases := map[string]string{
        "en":    "ltr",
        "fr-CA": "ltr",
        "ar":    "rtl",
        "he":    "rtl",
        "fa":    "rtl",
        "ja":    "ltr",
    }

    for tag, expected := range cases {
        got := TextDirection(language.MustParse(tag))
        if got != expected {
            t.Errorf("%q: got dir %q, want %q", tag, got, expected)
        }
    }
}

func assertNoPasteStored(t *testing.T, folder string) {
    t.Helper()

    entries, err := os.ReadDir(folder)
    if nil != err {
        t.Fatalf("fail to read the paste folder: %s", err)
    }

    if 0 != len(entries) {
        t.Errorf("%d paste stored, want none", len(entries))
    }
}
