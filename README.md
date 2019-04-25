# OpenGoPaste

<img src="https://leblanc.io/logo-open-go-paste.svg" width="200" height="200" title="OpenGoPaste Logo">


## Installation

```bash
go get github.com/gorilla/mux
```

## Usage

### Basic launch

If your $GOPATH/bin is in your PATH, you can simply:

```bash
open-go-server
```
The server will be launched at `http://127.0.0.1:8080` and the datas directory will be `../datas` relative by current working drectory.

### Options

All options are managed by environment variables.

* Customize listen IP : `OPEN_GO_PASTE_SERVER=0.0.0.0`
* Customize listen port : `OPEN_GO_PASTE_PORT=3000`
* Customize datas directory : `OPEN_GO_PASTE_DATAS_FOLDER=/var/open-go-paste/datas`


## Thanks
* https://highlightjs.org/
* https://github.com/showdownjs/showdown
* https://ionicons.com/ (Copy icon is licensed under MIT)
