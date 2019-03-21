package main

import (
  "html/template"
  "net/http"
  "path/filepath"
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

func (v *View) Render(w http.ResponseWriter, data interface{}) error {
    return v.Template.ExecuteTemplate(w, v.Layout, data)
}
