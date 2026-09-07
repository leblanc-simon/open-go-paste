package main

import (
    "bytes"
    "embed"
    "io/fs"
    "log"
    "net/http"
    "os"
    "strings"

    "github.com/gorilla/mux"
)

// assetsFS carries the stylesheets, scripts, fonts and images inside the binary.
// Everything the service needs to run is in there; only the paste storage and,
// when there is one, the operator stylesheet live outside.
//
// The stylesheets are listed one by one on purpose: "assets/css" would also pick
// up the git ignored custom.css of whoever runs the build, and ship it in the
// release. TestTheEmbeddedAssetsMatchTheAssetsFolder guards the list.
//
//go:embed assets/js assets/img assets/fonts assets/css/highlightjs
//go:embed assets/css/about.css assets/css/app.css assets/css/color.css assets/css/error.css
var assetsFS embed.FS

// customCssName is the one asset deliberately left out of the binary.
const customCssName = "assets/css/custom.css"

func getServerParameters() bytes.Buffer {
    defaultServer := "127.0.0.1"
    envServer := os.Getenv("OPEN_GO_PASTE_SERVER")
    defaultPort := ":8080"
    envPort := os.Getenv("OPEN_GO_PASTE_PORT")

    var server bytes.Buffer
    if envServer != "" {
        server.WriteString(envServer)
    } else {
        server.WriteString(defaultServer)
    }

    if envPort != "" {
        server.WriteString(":")
        server.WriteString(envPort)
    } else {
        server.WriteString(defaultPort)
    }

    return server
}

// overlayFileSystem serves the embedded assets, with a folder on disk taking
// precedence over them. This is what keeps OPEN_GO_PASTE_CUSTOM_CSS usable: the
// operator stylesheet is written at deployment time, it cannot be part of the
// binary. It also lets the front end be edited without rebuilding.
type overlayFileSystem struct {
    embedded http.FileSystem
    overlay  http.FileSystem
}

func (o overlayFileSystem) Open(name string) (http.File, error) {
    if nil != o.overlay {
        file, err := o.overlay.Open(name)
        if nil == err {
            return file, nil
        }
    }

    return o.embedded.Open(name)
}

// assetsOverlay is the optional folder overriding the embedded assets, empty
// when the operator did not configure one.
func assetsOverlay() http.FileSystem {
    folder := os.Getenv("OPEN_GO_PASTE_ASSETS_FOLDER")
    if "" == folder {
        return nil
    }

    return http.Dir(folder)
}

func embeddedAssets() http.FileSystem {
    // The embedded tree is rooted at "assets", the urls are not.
    assets, err := fs.Sub(assetsFS, "assets")
    if nil != err {
        // Only reachable if the embed directive and this path disagree.
        panic(err)
    }

    return http.FS(assets)
}

func assetsDelivery(r *mux.Router) {
    fileSystem := overlayFileSystem{
        embedded: embeddedAssets(),
        overlay:  assetsOverlay(),
    }

    r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(fileSystem)))
}

// checkCustomCss traces a stylesheet that will 404 at the first page load: with
// the assets embedded, a "/static/..." path only resolves if the file is either
// in the binary or in OPEN_GO_PASTE_ASSETS_FOLDER.
func checkCustomCss() {
    customCss := os.Getenv("OPEN_GO_PASTE_CUSTOM_CSS")
    if "" == customCss || !strings.HasPrefix(customCss, "/static/") {
        return
    }

    name := strings.TrimPrefix(customCss, "/static/")

    file, err := overlayFileSystem{
        embedded: embeddedAssets(),
        overlay:  assetsOverlay(),
    }.Open("/" + name)

    if nil != err {
        log.Printf(
            "Custom CSS %s is served by no asset: put the file in OPEN_GO_PASTE_ASSETS_FOLDER, or use an absolute url",
            customCss,
        )
        return
    }

    file.Close()
}

func RunServer(r *mux.Router) {
    server := getServerParameters()
    assetsDelivery(r)
    checkCustomCss()

    if folder := os.Getenv("OPEN_GO_PASTE_ASSETS_FOLDER"); "" != folder {
        log.Printf("Assets in %s take precedence over the embedded ones", folder)
    }

    log.Printf("Running server %s", server.String())
    http.ListenAndServe(server.String(), r)
}
