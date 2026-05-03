package transport

import (
	"io/fs"
	"net/http"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	web "github.com/noopolis/moltnet/web"
)

func attachUIRoutes(mux *http.ServeMux, policy *authn.Policy, verifier agentTokenVerifier) {
	staticFiles, err := fs.Sub(web.Files, "dist")
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
	spa := spaFallback(staticFiles, fileServer)

	mux.HandleFunc("GET /", func(response http.ResponseWriter, request *http.Request) {
		if maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}
		http.Redirect(response, request, "/console/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("GET /console/", authorizedConsole(policy, verifier, func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/console/" && maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}
		http.StripPrefix("/console/", spa).ServeHTTP(response, request)
	}))
	mux.HandleFunc("GET /console", authorizedConsole(policy, verifier, func(response http.ResponseWriter, request *http.Request) {
		if maybeSetConsoleAuthCookie(policy, response, request) {
			return
		}
		http.Redirect(response, request, "/console/", http.StatusTemporaryRedirect)
	}))
}

// spaFallback serves built assets when they exist; otherwise it rewrites the
// request to "/" so the SPA's index.html is returned. This lets client-side
// routes (e.g. /console/room/lobby) survive a hard refresh.
func spaFallback(files fs.FS, fileServer http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		path := strings.TrimPrefix(request.URL.Path, "/")
		if path == "" {
			fileServer.ServeHTTP(response, request)
			return
		}
		if _, err := fs.Stat(files, path); err == nil {
			fileServer.ServeHTTP(response, request)
			return
		}
		clone := request.Clone(request.Context())
		clone.URL.Path = "/"
		fileServer.ServeHTTP(response, clone)
	})
}
