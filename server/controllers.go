package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gorilla/mux"
)

var errorView *View
var about *View
var index *View
var paste *View

func InitTemplateViews() {
    errorView = NewView("layout", "templates/views/error.gohtml")
    about = NewView("layout", "templates/views/about.gohtml")
    index = NewView("layout", "templates/views/index.gohtml")
    paste = NewView("layout", "templates/views/paste.gohtml")
}

type ErrorData struct {
    Title string
    Message string
}
func ErrorHandler(w http.ResponseWriter, error string, code int) {
    var errorData ErrorData
    errorData.Message = error

    if code == 400 {
        errorData.Title = locale.t("400 Bad Request")
    } else if code == 404 {
        errorData.Title = locale.t("404 Not Found")
    } else if code == 405 {
        errorData.Title = locale.t("405 Method Not Allowed")
    }

    w.WriteHeader(code)
    errorView.Render(w, errorData)
}

func NotFoundController(w http.ResponseWriter, r *http.Request) {
    locale = NewLocale(r)
    ErrorHandler(w, locale.t("404 Not Found"), 404)
}

func AboutController(w http.ResponseWriter, r *http.Request) {
    locale = NewLocale(r)
    about.Render(w, nil)
}

type IndexData struct {
    AllowedTimes []DurationTime
    AllowedTypes []PasteTypeStruct
}
func IndexController(w http.ResponseWriter, r *http.Request) {
    locale = NewLocale(r)
    var maxTime MaxTime
    var pasteType PasteType

    index.Render(w, IndexData{AllowedTimes: maxTime.GetAllowed(), AllowedTypes: pasteType.GetAllowed()})
}

func IsValidUserAgent(r *http.Request) bool {
    userAgent := r.UserAgent()
    matchMozilla, _ := regexp.MatchString("^Mozilla/", userAgent)
    matchBot, _ := regexp.MatchString("http", userAgent)
    matchFuckingStupidMicrosoft, _ := regexp.MatchString("SkypeUriPreview", userAgent)

    return matchMozilla && !matchBot && !matchFuckingStupidMicrosoft
}

func GetPasteController(w http.ResponseWriter, r *http.Request) {
    locale = NewLocale(r)
    vars := mux.Vars(r)
    pasteId := vars["pasteId"]

    pasteJson, err := ReadPaste(pasteId, IsValidUserAgent(r))

    if (nil != err) {
        ErrorHandler(w, locale.t("this paste is not found"), http.StatusNotFound)
        return
    }

    paste.Render(w, pasteJson)
}

func PostPasteController(w http.ResponseWriter, r *http.Request) {
    locale = NewLocale(r)
    pasteContent := r.FormValue("paste")
    pasteType := r.FormValue("type")
    pasteMaxTime := r.FormValue("max_time")
    pasteMaxRead, err := strconv.Atoi(r.FormValue("max_read"))

    match, _ := regexp.MatchString("^[0-9,]+$", pasteContent)
    if (false == match) {
        ErrorHandler(w, locale.t("Invalid paste content"), http.StatusBadRequest)
        return
    }

    if nil != err || pasteMaxRead < 0 || pasteMaxRead > 500 {
        ErrorHandler(w, locale.t("max_read value must be a positive int"), http.StatusBadRequest)
        return
    }

    var structMaxTime MaxTime
    if !structMaxTime.HasAllowed(pasteMaxTime) {
        ErrorHandler(w, locale.t("this max_time value is not allowed"), http.StatusBadRequest)
        return
    }

    var structPasteType PasteType
    if !structPasteType.HasAllowed(pasteType) {
        ErrorHandler(w, locale.t("this type value is not allowed"), http.StatusBadRequest)
        return
    }

    pasteId, err := CreatePaste(pasteContent, pasteType, pasteMaxTime, pasteMaxRead)
    if nil != err {
        ErrorHandler(w, err.Error(), http.StatusBadRequest)
        return
    }

    fmt.Fprintf(w, "%s", pasteId)
}
