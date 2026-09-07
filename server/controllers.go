package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

var errorView *View
var aboutView *View
var indexView *View
var pasteView *View

// Pastes are encrypted by the browser and posted as a comma separated list of
// bytes.
var pasteContentPattern = regexp.MustCompile(`^[0-9,]+$`)

func InitTemplateViews() {
	errorView = NewView("layout", "templates/views/error.gohtml")
	aboutView = NewView("layout", "templates/views/about.gohtml")
	indexView = NewView("layout", "templates/views/index.gohtml")
	pasteView = NewView("layout", "templates/views/paste.gohtml")
}

// render sends the view to the client, falling back to a plain 500 when the
// template itself fails. Render leaves the response untouched in that case.
func render(w http.ResponseWriter, view *View, locale *Locale, data interface{}, code int) {
	if err := view.Render(w, locale, data, code); nil != err {
		log.Printf("Fail to render view: %s", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

type ErrorData struct {
	Title   string
	Message string
}

func errorTitle(locale *Locale, code int) string {
	switch code {
	case http.StatusBadRequest:
		return locale.t("400 Bad Request")
	case http.StatusNotFound:
		return locale.t("404 Not Found")
	case http.StatusMethodNotAllowed:
		return locale.t("405 Method Not Allowed")
	}

	return http.StatusText(code)
}

func ErrorHandler(w http.ResponseWriter, locale *Locale, message string, code int) {
	errorData := ErrorData{
		Title:   errorTitle(locale, code),
		Message: message,
	}

	render(w, errorView, locale, errorData, code)
}

func NotFoundController(w http.ResponseWriter, r *http.Request) {
	locale := NewLocale(r)
	ErrorHandler(w, locale, locale.t("404 Not Found"), http.StatusNotFound)
}

func AboutController(w http.ResponseWriter, r *http.Request) {
	render(w, aboutView, NewLocale(r), nil, http.StatusOK)
}

type IndexData struct {
	AllowedTimes []DurationTime
	AllowedTypes []PasteTypeStruct
}

func IndexController(w http.ResponseWriter, r *http.Request) {
	locale := NewLocale(r)
	var maxTime MaxTime
	var pasteType PasteType

	indexData := IndexData{
		AllowedTimes: maxTime.GetAllowed(locale),
		AllowedTypes: pasteType.GetAllowed(locale),
	}

	render(w, indexView, locale, indexData, http.StatusOK)
}

// IsValidUserAgent tells real browsers apart from crawlers and link previewers,
// which must not consume a read.
func IsValidUserAgent(r *http.Request) bool {
	userAgent := r.UserAgent()

	isBrowser := strings.HasPrefix(userAgent, "Mozilla/")
	isCrawler := strings.Contains(userAgent, "http")
	isFuckingStupidMicrosoft := strings.Contains(userAgent, "SkypeUriPreview")

	return isBrowser && !isCrawler && !isFuckingStupidMicrosoft
}

func GetPasteController(w http.ResponseWriter, r *http.Request) {
	locale := NewLocale(r)
	vars := mux.Vars(r)
	pasteId := vars["pasteId"]

	pasteData, err := ReadPaste(pasteId, IsValidUserAgent(r))

	if nil != err {
		// A missing or expired paste is the nominal case, only trace the rest.
		if !errors.Is(err, ErrPasteNotFound) && !errors.Is(err, ErrPasteExpired) {
			log.Printf("Fail to read paste: %s", err)
		}

		ErrorHandler(w, locale, locale.t("this paste is not found"), http.StatusNotFound)
		return
	}

	render(w, pasteView, locale, pasteData, http.StatusOK)
}

func PostPasteController(w http.ResponseWriter, r *http.Request) {
	locale := NewLocale(r)
	pasteContent := r.FormValue("paste")
	pasteType := r.FormValue("type")
	pasteMaxTime := r.FormValue("max_time")
	pasteMaxRead, err := strconv.Atoi(r.FormValue("max_read"))

	if !pasteContentPattern.MatchString(pasteContent) {
		ErrorHandler(w, locale, locale.t("Invalid paste content"), http.StatusBadRequest)
		return
	}

	if nil != err || pasteMaxRead < 0 || pasteMaxRead > 500 {
		ErrorHandler(w, locale, locale.t("max_read value must be a positive int"), http.StatusBadRequest)
		return
	}

	var structMaxTime MaxTime
	if !structMaxTime.HasAllowed(pasteMaxTime) {
		ErrorHandler(w, locale, locale.t("this max_time value is not allowed"), http.StatusBadRequest)
		return
	}

	var structPasteType PasteType
	if !structPasteType.HasAllowed(pasteType) {
		ErrorHandler(w, locale, locale.t("this type value is not allowed"), http.StatusBadRequest)
		return
	}

	pasteId, err := CreatePaste(pasteContent, pasteType, pasteMaxTime, pasteMaxRead)
	if nil != err {
		ErrorHandler(w, locale, err.Error(), http.StatusBadRequest)
		return
	}

	fmt.Fprintf(w, "%s", pasteId)
}
