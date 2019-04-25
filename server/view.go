package main

import (
  "html/template"
  "net/http"
  "path/filepath"
  "os"
)

var LayoutDir string = "templates"

func layoutFiles() []string {
  files, err := filepath.Glob(LayoutDir + "/*.gohtml")
  if err != nil {
    panic(err)
  }
  return files
}


func NewView(layout string, files ...string) *View {
    files = append(files, layoutFiles()...)
    t, err := template.ParseFiles(files...)
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
}

func (v *View) Render(w http.ResponseWriter, data interface{}) error {
    customCss := os.Getenv("OPEN_GO_PASTE_CUSTOM_CSS")

    renderData := RenderData{Data: data, CustomCss: customCss}

    return v.Template.ExecuteTemplate(w, v.Layout, renderData)
}
