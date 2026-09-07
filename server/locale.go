package main

//go:generate gotext -srclang=en update -out=catalog/catalog.go -lang=en,fr

import (
    "net/http"

    _ "leblanc.io/open-go-paste/catalog"

    "golang.org/x/text/language"
    "golang.org/x/text/message"
)

var supportedLanguages = message.DefaultCatalog.Languages()

var matcher = language.NewMatcher(supportedLanguages)

// Locale holds the language negotiated for a single request. An instance is
// built per request and must never be shared between goroutines.
type Locale struct {
    Printer *message.Printer
    tag     language.Tag
}

func NewLocale(r *http.Request) *Locale {
    acceptLang := r.Header.Get("Accept-Language")
    tag, index := language.MatchStrings(matcher, "", acceptLang)

    // MatchStrings may return a tag carrying extensions (-u-rg-...) which have
    // no place in the lang attribute: keep the plain catalog tag for the markup.
    documentTag := tag
    if index >= 0 && index < len(supportedLanguages) {
        documentTag = supportedLanguages[index]
    }

    return &Locale{
        Printer: message.NewPrinter(tag),
        tag:     documentTag,
    }
}

func (locale Locale) t(text string) string {
    return locale.Printer.Sprintf(text)
}

// Lang returns the BCP 47 tag to use in the lang attribute of the document.
func (locale Locale) Lang() string {
    return locale.tag.String()
}
