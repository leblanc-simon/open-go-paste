package main

import (
  "bytes"
  "embed"
  "html/template"
  "io/fs"
  "log/slog"
  "net/http"
)

var LayoutDir string = "templates"

// templatesFS carries the views inside the binary, so the server no longer has
// to be started from the directory holding them.
//
//go:embed templates
var templatesFS embed.FS

func layoutFiles() []string {
  files, err := fs.Glob(templatesFS, LayoutDir + "/*.gohtml")
  if err != nil {
    panic(err)
  }
  return files
}


func NewView(layout string, files ...string) *View {
    files = append(files, layoutFiles()...)
    t := template.New(files[0])

    // "trans" has to exist at parse time, but its real implementation depends
    // on the language negotiated for the request: Render rebinds it on a clone.
    t = t.Funcs(template.FuncMap{
        "trans": func(text string) string {
            return text
        },
    })

    t, err := t.ParseFS(templatesFS, files...)
    if err != nil {
        panic(err)
    }

    return &View{
        Template: t,
        Layout:   layout,
    }
}

type View struct {
    Template *template.Template
    Layout   string
}

type RenderData struct {
    Data interface{}
    CustomCss string
    Locale *Locale
}

// Render writes the view for a single request. The parsed template is never
// executed as is: every request renders a clone carrying its own "trans"
// function, so no request state is shared between goroutines.
func (v *View) Render(w http.ResponseWriter, locale *Locale, data interface{}, code int) error {
    tmpl, err := v.Template.Clone()
    if err != nil {
        return err
    }

    tmpl = tmpl.Funcs(template.FuncMap{
        "trans": locale.t,
    })

    renderData := RenderData{
        Data: data,
        CustomCss: config.Paste.CustomCss,
        Locale: locale,
    }

    // Render into a buffer first: a template failure must not leave a half
    // written body behind an already committed status code.
    var body bytes.Buffer
    if err := tmpl.ExecuteTemplate(&body, v.Layout, renderData); err != nil {
        return err
    }

    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.WriteHeader(code)

    if _, err := body.WriteTo(w); err != nil {
        // The status code is already sent, tracing is all we can do here.
        slog.Error("fail to write response", "err", err)
    }

    return nil
}
