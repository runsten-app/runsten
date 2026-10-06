package api

import (
	"bytes"
	"html/template"
	"net/http"
)

// view is a server-rendered page. The forms and links are relative, like the
// redirects: the pages work under a reverse proxy prefix. Those of the home page are
// only on pages at the root of runsten-api (/, /login and /connection).
type view struct {
	Title    string
	Message  string
	Error    string
	Login    bool   // the sign-in form
	Username string // the signed-in home page
	Back     string // the way back to the Connection page (Server.appURL)
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Runsten · {{.Title}}</title>
<style>body{font:16px/1.5 system-ui,sans-serif;margin:0;padding:4rem 1rem;background:#f4f5f7;color:#1d2330}
main{margin:0 auto;max-width:32rem;padding:2rem;background:#fff;border-radius:12px;box-shadow:0 1px 3px #0002}
h1{margin:0 0 1rem;font-size:1.5rem}label{display:block;margin:.5rem 0}
input{display:block;font:inherit;padding:.4rem;width:100%;box-sizing:border-box}
button,.button{display:inline-block;font:inherit;padding:.4rem 1.2rem;margin-top:.5rem;border:0;border-radius:6px;
background:#1c6bb5;color:#fff;text-decoration:none;cursor:pointer}.error{color:#a4161a}
@media (prefers-color-scheme:dark){body{background:#15181e;color:#e3e6ec}main{background:#1f232b}.error{color:#ff8a8a}}</style>
</head><body><main><h1>{{.Title}}</h1>
{{- with .Message}}<p>{{.}}</p>{{end}}
{{- with .Error}}<p class="error" role="alert">{{.}}</p>{{end}}
{{- if .Login}}
<form method="post" action="login">
<label>Username <input name="username" autocomplete="username" autocapitalize="none" required autofocus></label>
<label>Password <input type="password" name="password" autocomplete="current-password" required></label>
<button>Sign in</button></form>
{{- end}}
{{- if .Username}}
<p>Signed in as <strong>{{.Username}}</strong>.</p>
<p><a href="auth/volvo/start">Connect a Volvo ID</a>, or connect it again after a
"re-authentication required".</p>
<form method="post" action="logout"><button>Sign out</button></form>
{{- end}}
{{- with .Back}}
<p><a class="button" href="{{.}}">Back to Runsten</a></p>
{{- end}}
</main></body></html>
`))

// render writes a page. The pages have no script, and may only post forms to
// runsten-api itself.
func render(w http.ResponseWriter, status int, v view) {
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, v); err != nil {
		http.Error(w, v.Title, http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
