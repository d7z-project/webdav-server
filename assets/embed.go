package assets

import (
	_ "embed"
	"text/template"
)

//go:embed z-index.tmpl
var zIndex string

//go:embed z-preview.tmpl
var zPreview string

var ZIndex *template.Template
var ZPreview *template.Template

func init() {
	var err error
	ZIndex, err = template.New("index").Parse(zIndex)
	if err != nil {
		panic(err)
	}
	ZPreview, err = template.New("preview").Parse(zPreview)
	if err != nil {
		panic(err)
	}
}
