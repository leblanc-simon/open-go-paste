package main

import (
	"fmt"
	"net/http"
    "regexp"
    "strconv"

    "github.com/gorilla/mux"
)

var notFound *View
var index *View
var paste *View

func InitTemplateViews() {
    notFound = NewView("layout", "templates/views/404.gohtml")
    index = NewView("layout", "templates/views/index.gohtml")
    paste = NewView("layout", "templates/views/paste.gohtml")
}

func NotFoundController(w http.ResponseWriter, r *http.Request) {
    notFound.Render(w, nil)
}

type IndexData struct {
    AllowedTimes []DurationTime
    AllowedTypes []string
}
func IndexController(w http.ResponseWriter, r *http.Request) {
    var maxTime MaxTime
    var pasteType PasteType

    index.Render(w, IndexData{AllowedTimes: maxTime.GetAllowed(), AllowedTypes: pasteType.GetAllowed()})
}

func GetPasteController(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    pasteId := vars["pasteId"]

    pasteJson, err := ReadPaste(pasteId)

    if (nil != err) {
        http.Error(w, "this paste is not found", http.StatusNotFound)
        return
    }

    paste.Render(w, pasteJson)
}

func PostPasteController(w http.ResponseWriter, r *http.Request) {
    pasteContent := r.FormValue("paste")
    pasteType := r.FormValue("type")
    pasteMaxTime := r.FormValue("max_time")
    pasteMaxRead, err := strconv.Atoi(r.FormValue("max_read"))

    match, _ := regexp.MatchString("^[0-9,]+$", pasteContent)
    if (false == match) {
        http.Error(w, "Invalid paste content", http.StatusBadRequest)
        return
    }

    if nil != err || pasteMaxRead < 0 || pasteMaxRead > 500 {
        http.Error(w, "max_read value must be a positive int", http.StatusBadRequest)
        return
    }

    var structMaxTime MaxTime
    if (false == structMaxTime.HasAllowed(pasteMaxTime)) {
        http.Error(w, "this max_time value is not allowed", http.StatusBadRequest)
        return
    }

    var structPasteType PasteType
    if (false == structPasteType.HasAllowed(pasteType)) {
        http.Error(w, "this type value is not allowed", http.StatusBadRequest)
        return
    }

    pasteId, err := CreatePaste(pasteContent, pasteType, pasteMaxTime, pasteMaxRead)
    if (nil != err) {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    fmt.Fprintf(w, "%s", pasteId)
}
