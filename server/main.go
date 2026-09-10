package main

import (
    "log/slog"
    "net/http"
    "os"

    "github.com/gorilla/mux"
)

func main() {
    LoadConfig()
    InitTemplateViews()
    InitPasteFolder()

    // Scheduled synchronously: chrono only registers the task, it does not
    // block. Started in a goroutine with its error dropped, a malformed cron
    // expression left the service running with no cleanup pass at all.
    if err := InitScheduler(); nil != err {
        os.Exit(1)
    }

    r := mux.NewRouter()

    r.HandleFunc("/", IndexController).Methods("GET")
    r.HandleFunc("/p/{pasteId}", GetPasteController).Methods("GET")
    r.HandleFunc("/", PostPasteController).Methods("POST")
    r.HandleFunc("/about", AboutController).Methods("GET")

    r.NotFoundHandler = http.HandlerFunc(NotFoundController)

    if err := RunServer(r); nil != err {
        slog.Error("server stopped", "err", err)
        os.Exit(1)
    }
}
