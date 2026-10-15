package httputil

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func OK(w http.ResponseWriter, data any) {
	Write(w, http.StatusOK, Envelope{Code: 0, Message: "success", Data: data})
}

func Error(w http.ResponseWriter, status, code int, message string) {
	Write(w, status, Envelope{Code: code, Message: message, Data: nil})
}

func Write(w http.ResponseWriter, status int, body Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func Decode(r *http.Request, dest any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dest)
}
