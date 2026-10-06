package nominatim

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"runsten/internal/core"
)

var lyon = core.Position{Lat: 45.764, Lon: 4.8357}

func serve(t *testing.T, status int, body string) (*Client, *http.Request) {
	t.Helper()
	var got http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/nominatim/?key=secret", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c, &got
}

func TestReverse(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"street and city", `{"display_name":"12, Rue de la République, Lyon, France","address":{"house_number":"12","road":"Rue de la République","suburb":"2e Arrondissement","city":"Lyon","country":"France"}}`, "Rue de la République, Lyon"},
		{"a village", `{"address":{"road":"Route de Genas","village":"Chassieu"}}`, "Route de Genas, Chassieu"},
		{"no street", `{"address":{"suburb":"Croix-Luizet","city":"Villeurbanne"}}`, "Croix-Luizet, Villeurbanne"},
		{"the same twice", `{"address":{"place":"Lyon","city":"Lyon"}}`, "Lyon"},
		{"no details", `{"display_name":"Parc de la Tête d'Or, Lyon, Métropole de Lyon, France"}`, "Parc de la Tête d'Or, Lyon"},
		{"nothing there", `{"error":"Unable to geocode"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, req := serve(t, http.StatusOK, tt.body)
			got, err := c.Reverse(context.Background(), lyon)
			if err != nil || got != tt.want {
				t.Fatalf("Reverse = %q, %v, want %q", got, err, tt.want)
			}
			q := req.URL.Query()
			if req.URL.Path != "/nominatim/reverse" || q.Get("lat") != "45.764" || q.Get("lon") != "4.8357" ||
				q.Get("format") != "jsonv2" || q.Get("key") != "secret" {
				t.Errorf("request %s", req.URL)
			}
			if req.Header.Get("User-Agent") != UserAgent {
				t.Errorf("User-Agent %q", req.Header.Get("User-Agent"))
			}
		})
	}
}

func TestReverseFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		status int
		body   string
	}{
		"rate limited":  {http.StatusTooManyRequests, ""},
		"not json":      {http.StatusOK, "<html>"},
		"server broken": {http.StatusBadGateway, ""},
	} {
		c, _ := serve(t, tt.status, tt.body)
		if _, err := c.Reverse(context.Background(), lyon); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// Unreachable: the error does not tell the position.
	c, err := New("http://127.0.0.1:1", http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reverse(context.Background(), lyon); err == nil || strings.Contains(err.Error(), "45.764") {
		t.Errorf("unreachable: %v", err)
	}
	for _, bad := range []string{"", "nominatim.example", "ftp://nominatim.example", "https://"} {
		if _, err := New(bad, http.DefaultClient); err == nil {
			t.Errorf("New(%q): no error", bad)
		}
	}
}
