package main

import (
    "net/http"

    "github.com/gorilla/mux"
)

func main() {
    go InitScheduler()
    InitTemplateViews()
    InitPasteFolder()

    r := mux.NewRouter()

    r.HandleFunc("/", IndexController).Methods("GET")
    r.HandleFunc("/p/{pasteId}", GetPasteController).Methods("GET")
    r.HandleFunc("/", PostPasteController).Methods("POST")
    r.HandleFunc("/about", AboutController).Methods("GET")

    r.NotFoundHandler = http.HandlerFunc(NotFoundController)

    RunServer(r)
}
