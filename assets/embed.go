package assets

import (
	_ "embed"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/inhies/go-bytesize"
)

//go:embed z-index.tmpl.html
var zIndex string

//go:embed z-preview.tmpl.html
var zPreview string

//go:embed z-login.tmpl.html
var zLogin string

var (
	ZIndex   *template.Template
	ZPreview *template.Template
	ZLogin   *template.Template
)

func init() {
	var err error
	funcMap := sprig.FuncMap()
	funcMap["Bytesize"] = func(size int64) string {
		return bytesize.New(float64(size)).String()
	}

	ZIndex, err = template.New("index").Funcs(funcMap).Parse(zIndex)
	if err != nil {
		panic(err)
	}
	ZPreview, err = template.New("preview").Funcs(funcMap).Parse(zPreview)
	if err != nil {
		panic(err)
	}
	ZLogin, err = template.New("login").Funcs(funcMap).Parse(zLogin)
	if err != nil {
		panic(err)
	}
}
