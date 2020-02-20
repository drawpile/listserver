package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

type ResponseHandler func(*http.Request) http.Handler

func (f ResponseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f(r).ServeHTTP(w, r)
}

type JsonResponseHandler struct {
	Body       interface{}
	StatusCode int
}

func (jr JsonResponseHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	content, err := json.MarshalIndent(jr.Body, "", "\t")
	if err != nil {
		log.Println("JSON response marshalling error:", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))

	w.WriteHeader(jr.StatusCode)
	w.Write(content)
}

func JsonResponseOk(body interface{}) JsonResponseHandler {
	return JsonResponseHandler{body, http.StatusOK}
}

func ErrorResponse(message string, status int) http.Handler {
	return JsonResponseHandler{
		Body: map[string]string{
			"status":  "error",
			"message": message,
		},
		StatusCode: status,
	}
}
