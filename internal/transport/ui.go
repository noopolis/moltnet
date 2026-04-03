package transport

import (
	"io/fs"
	"net/http"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/web/uiassets"
)

func attachUIRoutes(mux *http.ServeMux, policy *authn.Policy) {
	staticFiles, err := fs.Sub(uiassets.Files, ".")
	if err != nil {
		assetUnavailable := func(response http.ResponseWriter, request *http.Request) {
			writeError(response, http.StatusInternalServerError, err)
		}
		mux.HandleFunc("GET /", assetUnavailable)
		mux.HandleFunc("GET /console", assetUnavailable)
		mux.HandleFunc("GET /console/", assetUnavailable)
		return
	}

	fileServer := http.FileServerFS(staticFiles)

	mux.HandleFunc("GET /", func(response http.ResponseWriter, request *http.Request) {
		if maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}
		http.Redirect(response, request, "/console/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("GET /console/", authorizedConsole(policy, func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/console/" && maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}
		http.StripPrefix("/console/", fileServer).ServeHTTP(response, request)
	}))
	mux.HandleFunc("GET /console", authorizedConsole(policy, func(response http.ResponseWriter, request *http.Request) {
		if maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}
		http.Redirect(response, request, "/console/", http.StatusTemporaryRedirect)
	}))
}
