package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewDevFrontendProxy proxies to the Vite development server.
//
// Without it, Vite would be a second origin: the browser would talk to 5173
// for the interface and to 7420 for the API, and exchanging the token for a
// cookie would not take the same path in development as in production. An
// authentication model that differs between the two is a model nobody ever
// really tests.
//
// With this proxy there is a single origin in both cases: the daemon serves
// the API under /api and delegates everything else. In production the
// embedded frontend takes the same place.
func NewDevFrontendProxy(target string, logger *slog.Logger) (http.Handler, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("unreadable development server address %q: %w", target, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf(
			"incomplete development server address %q: a scheme and a host are expected, for example http://127.0.0.1:5173",
			target)
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			// SetURL also rewrites the Host header to the target's, and
			// that is needed: Vite refuses requests whose host it does not
			// recognize, and the host arriving here is whatever the browser
			// used — 127.0.0.1, localhost, or the machine's own name.
			// Presenting the target's host is the right behavior for a proxy
			// to a local service anyway.
			request.SetURL(parsed)
		},
	}

	// Without this handler, a Vite that has not started yet yields a bare 502
	// and a blank page. The message says what is missing and where to look.
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, cause error) {
		logger.Error("cannot proxy to the development server",
			"target", target, "path", request.URL.Path, "error", cause)

		writeError(writer, logger, http.StatusBadGateway, fmt.Errorf(
			"the frontend development server is not answering on %s; "+
				"it comes up a few seconds after the daemon, otherwise check the output of ./do dev: %w",
			target, cause))
	}

	return proxy, nil
}
