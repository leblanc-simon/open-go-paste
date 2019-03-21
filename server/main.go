package main

import (
    "net/http"
    "github.com/gorilla/mux"
)

func main() {
    InitTemplateViews()
    InitPasteFolder()

    r := mux.NewRouter()

    r.HandleFunc("/", IndexController).Methods("GET")
    r.HandleFunc("/p/{pasteId}", GetPasteController).Methods("GET")
    r.HandleFunc("/", PostPasteController).Methods("POST")

    r.NotFoundHandler = http.HandlerFunc(NotFoundController)

    RunServer(r)
}
