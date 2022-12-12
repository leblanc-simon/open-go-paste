package main

//go:generate gotext -srclang=en update -out=catalog/catalog.go -lang=en,fr

import (
    "net/http"

    _ "leblanc.io/open-go-paste/catalog"

    "golang.org/x/text/language"
    "golang.org/x/text/message"
)

var matcher = language.NewMatcher(message.DefaultCatalog.Languages())

type Locale struct {
    Printer *message.Printer
}

func NewLocale(r *http.Request) *Locale {
    acceptLang := r.Header.Get("Accept-Language")
    tag, _ := language.MatchStrings(matcher, "", acceptLang)
    printer := message.NewPrinter(tag)

    return &Locale{
        Printer: printer,
    }
}

func (locale Locale) t(text string) string {
    return locale.Printer.Sprintf(text)
}

var locale *Locale
