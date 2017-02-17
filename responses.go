package main

import (
	"encoding/json"
	"net/http"
)

type apiResponse interface {
	WriteResponse(http.ResponseWriter)
}

type genericErrorResponse struct {
	Code    int
	Message string
}

func (r genericErrorResponse) WriteResponse(w http.ResponseWriter) {
	msg := make(map[string]string)
	msg["status"] = "error"
	msg["message"] = r.Message
	writeJsonResponse(w, msg, r.Code)
}

func writeJsonResponse(w http.ResponseWriter, body interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	content, _ := json.MarshalIndent(body, "", "  ")
	w.Write(content)
}

func notFoundResponse() genericErrorResponse {
	return genericErrorResponse{http.StatusNotFound, "Not found"}
}

func forbiddenResponse() genericErrorResponse {
	return genericErrorResponse{http.StatusForbidden, "Forbidden"}
}

func internalServerError() genericErrorResponse {
	return genericErrorResponse{http.StatusInternalServerError, "Internal Server Error"}
}

func badRequestResponse(message string) genericErrorResponse {
	return genericErrorResponse{http.StatusBadRequest, message}
}

func invalidDataResponse(message string) genericErrorResponse {
	return genericErrorResponse{http.StatusUnprocessableEntity, message}
}

func conflictResponse(message string) genericErrorResponse {
	return genericErrorResponse{http.StatusConflict, message}
}

type methodNotAllowedResponse struct {
	allow string
}

func (r methodNotAllowedResponse) WriteResponse(w http.ResponseWriter) {
	w.Header().Set("Allow", r.allow)
	genericErrorResponse{http.StatusMethodNotAllowed, "Method not allowed"}.WriteResponse(w)
}

type noContentResponse struct {
}

func (r noContentResponse) WriteResponse(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
