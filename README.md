# OpenGoPaste

<p align="center">
    <img src="https://leblanc.io/logo-open-go-paste.svg" width="200" height="200" title="OpenGoPaste Logo">
</p>

## Installation

```bash
go get github.com/gorilla/mux
go get -u golang.org/x/text/cmd/gotext
git clone https://github.com/leblanc-simon/open-go-paste
cd open-go-paste/server
go run *.go
```

## Usage

### Basic launch

First, build binary :

```bash
cd open-go-paste/server
go build -o open-go-server *.go
```

Then, launch server :

```bash
./open-go-server
```

The server will be launched at `http://127.0.0.1:8080` and the datas directory will be `../datas` relative to the current working drectory.

### Options

All options are managed by environment variables.

* Customize listen IP : `OPEN_GO_PASTE_SERVER=0.0.0.0`
* Customize listen port : `OPEN_GO_PASTE_PORT=3000`
* Customize datas directory : `OPEN_GO_PASTE_DATAS_FOLDER=/var/open-go-paste/datas`
* Customize CSS : `OPEN_GO_PASTE_CUSTOM_CSS="/static/css/custom.css"`

### Internationalisation

In template, we use the function `trans`. To extract string, use `release/extract-message-from-template.sh`

## Thanks

* https://highlightjs.org/
* https://github.com/showdownjs/showdown
* https://ionicons.com/ (Copy icon is licensed under MIT)

## Authors

* Simon Leblanc <contact@leblanc-simon.eu> (Code and design)
* Lucie Lenfant (Logos)

## License

* Code : [WTFPL](http://www.wtfpl.net/)
* Logos : [Art Libre](http://artlibre.org/)
