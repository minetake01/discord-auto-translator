package legalpages

import (
	_ "embed"
	"net/http"
)

const robotsDirectiveValue = "noindex, nofollow, noarchive, nosnippet"

var (
	//go:embed assets/privacy.html
	privacyHTML []byte

	//go:embed assets/terms.html
	termsHTML []byte

	//go:embed assets/styles.css
	stylesCSS []byte
)

// Register adds the embedded legal pages and their shared stylesheet to mux.
func Register(mux *http.ServeMux) {
	privacy := staticHandler("text/html; charset=utf-8", privacyHTML)
	terms := staticHandler("text/html; charset=utf-8", termsHTML)

	mux.Handle("GET /privacy", privacy)
	mux.Handle("GET /privacy/{$}", privacy)
	mux.Handle("GET /terms", terms)
	mux.Handle("GET /terms/{$}", terms)
	mux.Handle("GET /legal-assets/styles.css", staticHandler("text/css; charset=utf-8", stylesCSS))
}

func staticHandler(contentType string, content []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Robots-Tag", robotsDirectiveValue)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(content)
	})
}
