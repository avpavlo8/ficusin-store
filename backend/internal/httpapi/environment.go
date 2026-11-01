package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const stagingCookieName = "__Host-ficusin_staging"
const stagingLoginPath = "/__staging/login"
const stagingSessionDays = 7

var stagingLoginPage = template.Must(template.New("staging-login").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Вход на стейджинг · Фикусин</title><style>body{margin:0;background:#f5f5f0;color:#203729;font:16px system-ui,sans-serif;display:grid;min-height:100dvh;place-items:center}main{box-sizing:border-box;width:min(92vw,420px);padding:32px;background:white;border:1px solid #dae1d7;border-radius:16px}h1{font-size:25px;margin:8px 0 16px}p{line-height:1.5;color:#526052}label{display:block;margin:18px 0 6px}input,button{box-sizing:border-box;font:inherit;width:100%;padding:12px;border-radius:8px;border:1px solid #a9b7a6}button{margin-top:24px;background:#254e34;color:white;cursor:pointer}.error{color:#9f3027}</style><main><small>ФИКУСИН · ТЕСТОВЫЙ САЙТ</small><h1>Вход на стейджинг</h1><p>Введите доступ к тестовому сайту. Браузер запомнит его на 7 дней.</p>{{if .Error}}<p class="error" role="alert">Неверный логин или пароль.</p>{{end}}<form method="post" action="/__staging/login"><input type="hidden" name="next" value="{{.Next}}"><label for="username">Логин</label><input id="username" name="username" autocomplete="username" required value="staging"><label for="password">Пароль</label><input id="password" name="password" type="password" autocomplete="current-password" required><button type="submit">Войти</button></form></main></html>`))

func stagingSignature(token, expiry string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte("ficusin-staging-access-v1:" + expiry))
	return hex.EncodeToString(mac.Sum(nil))
}

func stagingCookieValid(r *http.Request, token string) bool {
	cookie, err := r.Cookie(stagingCookieName)
	if err != nil || token == "" {
		return false
	}
	expiry, signature, found := strings.Cut(cookie.Value, ".")
	until, err := strconv.ParseInt(expiry, 10, 64)
	if !found || err != nil || until <= time.Now().Unix() {
		return false
	}
	expected := stagingSignature(token, expiry)
	return hmac.Equal([]byte(signature), []byte(expected))
}

func setStagingCookie(w http.ResponseWriter, token string) {
	expires := time.Now().Add(stagingSessionDays * 24 * time.Hour)
	expiry := strconv.FormatInt(expires.Unix(), 10)
	http.SetCookie(w, &http.Cookie{Name: stagingCookieName, Value: expiry + "." + stagingSignature(token, expiry), Path: "/", Expires: expires, MaxAge: stagingSessionDays * 86400, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func stagingNext(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n") || parsed.IsAbs() || parsed.Host != "" || parsed.Path == stagingLoginPath {
		return "/"
	}
	return value
}

// The staging password establishes a signed browser session. HTTP Basic
// challenges must not be used: ordinary application 401s otherwise cause
// browsers to ask for the environment password repeatedly.
func EnvironmentGuard(next http.Handler, environment, accessToken string) http.Handler {
	if environment != "staging" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ficusin-Environment", "staging")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
			return
		}
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/ready" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == stagingLoginPath {
			destination := stagingNext(r.URL.Query().Get("next"))
			failed := false
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, 4096)
				if r.ParseForm() != nil {
					http.Error(w, "Некорректные данные", http.StatusBadRequest)
					return
				}
				destination = stagingNext(r.PostForm.Get("next"))
				if accessToken != "" && r.PostForm.Get("username") == "staging" && subtle.ConstantTimeCompare([]byte(r.PostForm.Get("password")), []byte(accessToken)) == 1 {
					setStagingCookie(w, accessToken)
					http.Redirect(w, r, destination, http.StatusSeeOther)
					return
				}
				failed = true
			} else if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET, POST")
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
			if failed {
				w.WriteHeader(http.StatusForbidden)
			}
			_ = stagingLoginPage.Execute(w, struct {
				Next  string
				Error bool
			}{destination, failed})
			return
		}
		if stagingCookieValid(r, accessToken) {
			next.ServeHTTP(w, r)
			return
		}
		// Explicit Basic headers remain supported for existing API probes, without
		// a WWW-Authenticate challenge or an environment-wide browser credential cache.
		username, password, ok := r.BasicAuth()
		if ok && accessToken != "" && username == "staging" && subtle.ConstantTimeCompare([]byte(password), []byte(accessToken)) == 1 {
			setStagingCookie(w, accessToken)
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusForbidden, errorResponse{Error: "Войдите на тестовый сайт"})
			return
		}
		http.Redirect(w, r, stagingLoginPath+"?next="+url.QueryEscape(stagingNext(r.URL.RequestURI())), http.StatusSeeOther)
	})
}
