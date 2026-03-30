package transport

import (
	"io/fs"
	"net/http"

	"github.com/noopolis/moltnet/web/uiassets"
)

func attachUIRoutes(mux *http.ServeMux) {
	staticFiles, err := fs.Sub(uiassets.Files, ".")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServerFS(staticFiles)

	mux.HandleFunc("GET /", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/ui/", http.StatusTemporaryRedirect)
	})
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", fileServer))
	mux.HandleFunc("GET /ui", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/ui/", http.StatusTemporaryRedirect)
	})
}
