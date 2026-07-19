package legalpages

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const robotsDirective = "noindex, nofollow, noarchive, nosnippet"

func TestRegisterServesEmbeddedLegalSite(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	pages := []struct {
		path       string
		wantMarker string
	}{
		{path: "/privacy", wantMarker: "AutoTranslator プライバシーポリシー"},
		{path: "/privacy/", wantMarker: "AutoTranslator プライバシーポリシー"},
		{path: "/terms", wantMarker: "AutoTranslator 利用規約"},
		{path: "/terms/", wantMarker: "AutoTranslator 利用規約"},
	}

	for _, page := range pages {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+page.path, func(t *testing.T) {
				response := serve(t, mux, method, page.path)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
				}
				if got := response.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
					t.Fatalf("Content-Type = %q", got)
				}
				if got := response.Header.Get("X-Robots-Tag"); got != robotsDirective {
					t.Fatalf("X-Robots-Tag = %q", got)
				}
				body := readBody(t, response)
				if method == http.MethodHead {
					if body != "" {
						t.Fatalf("HEAD body = %q, want empty", body)
					}
					return
				}
				for _, want := range []string{
					page.wantMarker,
					`<meta name="robots" content="` + robotsDirective + `">`,
					`<link rel="stylesheet" href="/legal-assets/styles.css">`,
				} {
					if !strings.Contains(body, want) {
						t.Errorf("body does not contain %q", want)
					}
				}
				if strings.Contains(strings.ToLower(body), "<script") {
					t.Error("body contains JavaScript")
				}
			})
		}
	}

	privacy := readBody(t, serve(t, mux, http.MethodGet, "/privacy"))
	for _, want := range []string{
		"Amazon Web Services（Amazon Bedrock）",
		"メッセージ本文、投稿者の表示名、会話履歴、サーバー名、チャンネル名、トピック、スレッド名、用語集および翻訳スタイル",
		"Google Gemma 4 26B",
		"google.gemma-4-26b-a4b",
		"us-west-2（米国西部・オレゴン）",
		"store: false",
		"使用モデル、入力・出力トークン数、処理ステータス・エラーおよび利用実績",
		"us-central1-a",
		"60日以内",
		"30日以内",
		"https://discord-translator.minetake.net/terms",
	} {
		if !strings.Contains(privacy, want) {
			t.Errorf("privacy page does not contain %q", want)
		}
	}
	for _, forbidden := range []string{"Cloudflare", "AI Gateway", "Workers AI"} {
		if strings.Contains(privacy, forbidden) {
			t.Errorf("privacy page still contains %q", forbidden)
		}
	}

	terms := readBody(t, serve(t, mux, http.MethodGet, "/terms"))
	if !strings.Contains(terms, "https://discord-translator.minetake.net/privacy") {
		t.Error("terms page is missing the privacy cross-link")
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method+" CSS", func(t *testing.T) {
			response := serve(t, mux, method, "/legal-assets/styles.css")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			if got := response.Header.Get("Content-Type"); got != "text/css; charset=utf-8" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := response.Header.Get("X-Robots-Tag"); got != robotsDirective {
				t.Fatalf("X-Robots-Tag = %q", got)
			}
			body := readBody(t, response)
			if method == http.MethodHead && body != "" {
				t.Fatalf("HEAD body = %q, want empty", body)
			}
			if method == http.MethodGet && !strings.Contains(body, ":root") {
				t.Error("CSS body does not contain shared stylesheet")
			}
		})
	}

	for _, path := range []string{"/", "/unknown", "/privacy/unknown", "/styles.css"} {
		response := serve(t, mux, http.MethodGet, path)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, response.StatusCode, http.StatusNotFound)
		}
		response.Body.Close()
	}
}

func serve(t *testing.T, handler http.Handler, method, path string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
