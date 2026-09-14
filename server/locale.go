package main

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"golang.org/x/text/language"
	"leblanc.io/open-go-base/i18n"
)

// rightToLeftScripts lists the scripts written from right to left. Only English
// and French ship today, so this never fires — it is here so that adding a
// catalogue is enough, without a layout that keeps claiming dir="ltr" and
// mirrors nothing.
var rightToLeftScripts = map[string]bool{
	"Adlm": true,
	"Arab": true,
	"Hebr": true,
	"Nkoo": true,
	"Rohg": true,
	"Samr": true,
	"Syrc": true,
	"Thaa": true,
	"Yezi": true,
}

// TextDirection answers what the dir attribute of the page has to say for a
// language. The script of a tag is often only inferred, which is fine here: a
// wrong guess flips the layout, it never hides anything.
func TextDirection(tag language.Tag) string {
	script, _ := tag.Script()

	if rightToLeftScripts[script.String()] {
		return "rtl"
	}

	return "ltr"
}

// localesFS carries the translation catalogues inside the binary, next to the
// templates and the assets: the service ships as a single file.
//
//go:embed locales/*.yaml
var localesFS embed.FS

// bundle holds every catalogue, read once at startup. It is safe for concurrent
// reads; a Localizer is negotiated per request and must not be shared between
// goroutines.
var bundle *i18n.Bundle

func InitLocales() {
	defaultLanguage := config.Paste.DefaultLanguage
	if "" == defaultLanguage {
		// Reached by the tests, which do not go through LoadConfig and the
		// env-default it applies.
		defaultLanguage = "en"
	}

	loaded, err := i18n.NewFS(localesFS, "locales", defaultLanguage)
	if nil != err {
		// A malformed catalogue, a reserved key used as a nesting level, or an
		// unknown default language. Every page of the service renders text:
		// none of them can be served without this.
		slog.Error("fail to load the translations", "err", err)
		os.Exit(1)
	}

	bundle = loaded

	slog.Info("translations loaded", "languages", bundle.Languages(), "default", defaultLanguage)
}

// NewLocale negotiates the language of a single request: ?lang= forces it,
// Accept-Language decides otherwise, the configured default language is the
// last resort.
func NewLocale(r *http.Request) *i18n.Localizer {
	return bundle.FromRequest(r)
}

// translationFuncs binds the catalogue of one request to the templates. T and Tn
// come from the bundle and are escaped like any other value.
//
// TH is for the handful of messages that carry their own markup, the footer and
// the licence lines of the about page: their links sit inside the sentence, and
// splitting them out would pin the word order to English. Both the message and
// the anchors are repo content, no paste data ever reaches them — which is what
// makes marking the result as template.HTML acceptable here.
func translationFuncs(locale *i18n.Localizer) template.FuncMap {
	funcs := template.FuncMap(locale.FuncMap())

	funcs["TH"] = func(messageID string, data ...any) template.HTML {
		return template.HTML(locale.T(messageID, data...))
	}

	return funcs
}
