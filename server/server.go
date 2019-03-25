package main

import (
    "bytes"
    "log"
    "net/http"
    "os"

    "github.com/gorilla/mux"
)

func getServerParameters() bytes.Buffer {
    defaultServer := "127.0.0.1"
    envServer := os.Getenv("OPEN_GO_PASTE_SERVER")
    defaultPort := ":8080"
    envPort := os.Getenv("OPEN_GO_PASTE_PORT")

    var server bytes.Buffer
    if ("" != envServer) {
        server.WriteString(envServer)
    } else {
        server.WriteString(defaultServer)
    }

    if ("" != envPort) {
        server.WriteString(":")
        server.WriteString(envPort)
    } else {
        server.WriteString(defaultPort)
    }

    return server
}

func assetsDelivery(r *mux.Router) {
    fs := http.FileServer(http.Dir("../assets/"))
    r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))
}

func RunServer(r *mux.Router) {
    server := getServerParameters()
    assetsDelivery(r)

    log.Printf("Running server %s", server.String())
    http.ListenAndServe(server.String(), r)
}
