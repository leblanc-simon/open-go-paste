# OpenGoPaste

<p align="center">
    <img src="server/assets/img/logo.svg" width="200" height="200" title="OpenGoPaste Logo">
</p>

## Installation

The Go dependencies are declared in `server/go.mod` and downloaded by the build
itself, there is nothing to install by hand:

```bash
git clone https://github.com/leblanc-simon/open-go-paste
cd open-go-paste/server
go run .
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

The templates and the assets are embedded in the binary (`//go:embed`), so it can
be deployed on its own: the paste storage is the only thing it needs next to it.

### Options

Options come from `config.yaml` when that file sits next to the binary, from the
environment otherwise. `-c <path>` points at another file, and `./open-go-paste
--help` prints the authoritative list of the variables with their defaults.

* Customize listen IP : `OPEN_GO_PASTE_HOST=0.0.0.0`
* Customize listen port : `OPEN_GO_PASTE_PORT=3000`
* Customize datas directory : `OPEN_GO_PASTE_DATAS_FOLDER=/var/open-go-paste/datas`
* Customize CSS : `OPEN_GO_PASTE_CUSTOM_CSS="/static/css/custom.css"` (redefine
  the tokens of `color.css`; keep every asset local, since a remote image or
  font is blocked by the CSP and would hand the address of each visitor to a
  third party)
* Override the embedded assets : `OPEN_GO_PASTE_ASSETS_FOLDER=/var/open-go-paste/assets`
* Customize clean datas CRON : `OPEN_GO_PASTE_CRON="0 0 0 * * *"` (CRON syntax with seconds)
* Requests per minute per client : `OPEN_GO_PASTE_RATE_LIMIT=100` (`0` disables the quota)
* Reverse proxy CIDRs : `OPEN_GO_PASTE_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16`
* Fallback language : `OPEN_GO_PASTE_DEFAULT_LANGUAGE=fr` (BCP 47 tag, defaults to `en`)
* Log level : `OPEN_GO_PASTE_LOG_LEVEL=info` (`debug|info|warn|error`, defaults to `error`)
* Log format : `OPEN_GO_PASTE_LOG_FORMAT=json` (`text|json`)
* Include the source position in the logs : `OPEN_GO_PASTE_LOG_SOURCE=true`

`OPEN_GO_PASTE_SERVER` was renamed `OPEN_GO_PASTE_HOST`. The old name still
works and prints a deprecation notice; it will be dropped in the next major
version.

The default log level is `error`, so a healthy start is silent. Set
`OPEN_GO_PASTE_LOG_LEVEL=info` to see the listen address, the resolved paste
folder and the cleanup schedule.

The same settings in a `config.yaml`:

```yaml
web:
  host: 0.0.0.0
  port: 3000
  rate_limit: 100
  trusted_proxies:
    - 10.0.0.0/8
logging:
  level: info
  format: json
paste:
  datas_folder: /var/open-go-paste/datas
  custom_css: /static/css/custom.css
  cron: "0 0 0 * * *"
  default_language: en
```

### Rate limiting

Creating a paste is anonymous and writes a file on disk, so a per-client quota
(`OPEN_GO_PASTE_RATE_LIMIT`, 100 requests per minute by default) is what stands
between a single client and a full storage. `/static/` is exempt: a page load
pulls a dozen assets, and counting those would burn the budget within a few
views. Over the quota, the answer is a `429` carrying `Retry-After`.

`X-Forwarded-For` is only honoured when the connection itself comes from a CIDR
listed in `OPEN_GO_PASTE_TRUSTED_PROXIES`, so a direct client cannot claim
another address. An empty list means "no proxy": behind an undeclared reverse
proxy, every client shares the quota of the proxy address.

### Custom CSS

`OPEN_GO_PASTE_CUSTOM_CSS` takes either an absolute url, served as is, or a
`/static/...` path. That path is resolved against the assets, and the assets live
in the binary: `custom.css` is written by the operator, so it is deliberately
left out of it. Point `OPEN_GO_PASTE_ASSETS_FOLDER` at a folder holding
`css/custom.css` and it will be served — that folder takes precedence over the
embedded assets, file by file, which is also handy to edit the front end without
rebuilding. The server traces a custom stylesheet that resolves to nothing at
startup, rather than letting the page 404 on it.

### Internationalisation

Translations live in `server/locales/<tag>.yaml`, one file per language, named
after its BCP 47 tag. They are embedded in the binary, so adding a language is a
matter of dropping a file next to the others and rebuilding — the available
languages are deduced from what is there, nothing else to declare.

Keys are in English and structured `screen.element`; the vocabulary shared by
several screens (`error.`, `validation.`, `duration.`, `paste_type.`) lives under
roots of its own. In the templates:

```gohtml
{{ T "index.save" }}                     {{/* escaped, the usual case */}}
{{ Tn "some.count" 3 }}                  {{/* plural form */}}
{{ TH "about.code_license" }}            {{/* message carrying its own markup */}}
```

`TH` is reserved for the handful of sentences whose links sit inside the text
(the footer, the licence lines of the about page): splitting the anchor out of
the sentence would pin the word order to English. Its result is not escaped, so
it must only ever be given a message from the catalogues.

Go code asks for a key through the request locale: `locale.T("error.not_found")`.
The labels of the durations and of the paste types are derived from their allowed
values, `duration.1D` and `paste_type.markdown` for instance, so the lists in
`paste.go` are the only place they are enumerated.

`?lang=fr` forces a language for one request, otherwise `Accept-Language`
decides, with `OPEN_GO_PASTE_DEFAULT_LANGUAGE` as the last resort. The `lang`
attribute of the document carries the complete negotiated tag, region extension
included: a `fr-CA` reader gets the French catalogue and `lang="fr-u-rg-cazzzz"`,
which is what tells the browser to format dates the Canadian way.

Beware of the identifiers `go-i18n` reserves inside a message (`description`,
`id`, `hash`, `translation`, `leftdelim`, `rightdelim` and the plural forms
`zero`, `one`, `two`, `few`, `many`, `other`): using one as a nesting level fails
the load at startup.

`TestEveryTranslationKeyResolvesInEveryLanguage` asks every language for every
key the templates and the controllers use, since a missing translation otherwise
renders as the key itself.

### Front end dependencies

`highlight.js` and `showdown` are committed under `assets/js`, and their versions
are declared in `package.json` so that a dependency scanner can see them. To
apply a new version, bump it in `package.json` then run `npm run vendor`
(`release/vendor-assets.sh`), and check a code paste and a markdown paste in a
browser.

### Tests

```bash
cd server && go test -race ./...   # server
npm install && npm test            # crypto and markdown sanitizer
```

## Thanks

* https://highlightjs.org/
* https://github.com/showdownjs/showdown
* https://ionicons.com/ (Copy and mail icons are licensed under MIT)

## Authors

* Simon Leblanc <contact@leblanc-simon.eu> (Code and design)
* Lucie Lenfant (Logos)

## License

* Code : [WTFPL](http://www.wtfpl.net/)
* Logos : [Art Libre](http://artlibre.org/)
