package api

import (
	"encoding/json"
	"net/http"
)

// writeError answers an error outside the operations, in their format: the /api/
// fallback, cross-origin refusals and the session check.
func writeError(w http.ResponseWriter, status int, code errorCode, message string) {
	body, err := json.Marshal(apiError(status, code, message))
	if err != nil { // a struct of strings: never
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
