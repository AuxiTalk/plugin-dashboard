package server

import (
	"net/http"

	"github.com/a-h/templ"
)

func render(component templ.Component) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		component.Render(r.Context(), w)
	}
}
