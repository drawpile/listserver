package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"log"
	"fmt"
	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/validation"
)

/**
 * API root: info about this server
 */
type apiRootResponse struct {
	ApiName string `json:"api_name"`
	Version string `json:"version"`
	Name string `json:"name"`
	Description string `json:"description"`
	Favicon string `json:"favicon,omitempty"`
	Source string `json:"source"`
}

func (r apiRootResponse) WriteResponse(w http.ResponseWriter) {
	writeJsonResponse(w, r, http.StatusOK)
}

func RootEndpoint(ctx *apiContext) apiResponse {
	if len(ctx.path) != 0 {
		return notFoundResponse()
	}
	if(ctx.method != "GET") {
		return methodNotAllowedResponse{"GET"}
	}

	return apiRootResponse{
		ApiName: "drawpile-listing",
		Version: "1.2",
		Name: ctx.cfg.Name,
		Description: ctx.cfg.Description,
		Favicon: ctx.cfg.Favicon,
		Source: "https://github.com/drawpile/listserver/",
	}
}

/**
 * List and edit sessions
 */

type SessionListResponse struct {
	sessions []db.SessionInfo
	jsonpCallback string
}

func (r SessionListResponse) WriteResponse(w http.ResponseWriter) {
	if len(r.jsonpCallback) == 0 || !validation.IsJsFunctionName(r.jsonpCallback) {
		writeJsonResponse(w, r.sessions, http.StatusOK)
	} else {
        w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		content, _ := json.MarshalIndent(r.sessions, "", "  ")
		fmt.Fprintf(w, "%s(%s);", r.jsonpCallback, content)
	}
}

func SessionListEndpoint(ctx *apiContext) apiResponse {
	if len(ctx.path) == 0 {
		if ctx.method == "GET" {
			return getSessionList(ctx)

		} else if ctx.method == "POST" {
			return postNewSession(ctx)
		} else {
			return methodNotAllowedResponse{"GET, POST"}
		}

	} else {
		id, err := strconv.Atoi(strings.TrimSuffix(ctx.path, "/"))
		if err != nil {
			return notFoundResponse()
		}

		if ctx.method == "PUT" {
			return refreshSession(id, ctx)
		} else if ctx.method == "DELETE" {
			return deleteSession(id, ctx)
		} else {
			return methodNotAllowedResponse{"PUT, DELETE"}
		}
	}
}

func getSessionList(ctx *apiContext) apiResponse {
	opts := db.QueryOptions{
		Title: ctx.query.Get("title"),
		Nsfm: ctx.query.Get("nsfm") == "true",
		Protocol: ctx.query.Get("protocol"),
	}
	list, err := db.QuerySessionList(opts, ctx.db)
	if err != nil {
		return internalServerError()
	}
	return SessionListResponse{list, ctx.query.Get("callback")}
}

type announcementResponse struct {
	*db.NewSessionInfo
	Expires int `json:"expires"`
	Message string `json:"message,omitempty"`
}

func (r announcementResponse) WriteResponse(w http.ResponseWriter) {
	writeJsonResponse(w, r, http.StatusOK)
}


func postNewSession(ctx *apiContext) apiResponse {
	var info db.SessionInfo
	if err := ctx.body.Decode(&info); err != nil {
		log.Println("Invalid session announcement body:", err)
		return badRequestResponse("Unparseable JSON request body")
	}

	// Validate
	rules := validation.AnnouncementValidationRules{
		ClientIP: ctx.clientIP,
		AllowWellKnownPorts: ctx.cfg.AllowWellKnownPorts,
		ProtocolWhitelist: ctx.cfg.ProtocolWhitelist,
	}
	if err := validation.ValidateAnnouncement(info, rules); err != nil {
		if _, isValidationError := err.(validation.ValidationError); isValidationError {
			return invalidDataResponse(err.Error())
		} else {
			return internalServerError()
		}
	}

	// Fill in default values
	if len(info.Host) == 0 {
		info.Host = ctx.clientIP.String()
	}
	if info.Port == 0 {
		info.Port = 27750
	}

	// Make sure this hasn't been announced yet
	if isActive, err := db.IsActiveSession(info.Host, info.Id, info.Port, ctx.db); err != nil {
		return internalServerError()
	} else if isActive {
		return conflictResponse("Session already listed")
	}

	// Check per-host session limit
	if !ctx.cfg.IsTrustedHost(info.Host) {
		if count, err := db.GetHostSessionCount(info.Host, ctx.db); err != nil {
			return internalServerError()
		} else if count >= ctx.cfg.MaxSessionsPerHost {
			return conflictResponse("Max listing count exceeded for this host")
		}
	}

	// Insert to database
	newses, err := db.InsertSession(info, ctx.clientIP.String(), ctx.db)
	if err != nil {
		return internalServerError()
	}

	log.Println(ctx.clientIP, "announced", newses.ListingId, info.Host, info.Port, info.Id)

	// TODO try connecting to the server to make sure the session actually exists

	return announcementResponse{
		&newses,
		db.SessionTimeout,
		ctx.cfg.Welcome,
	}
}

type refreshResponse struct {
	Status string `json:"status"`
	Message string `json:"message,omitempty"`
}

func (r refreshResponse) WriteResponse(w http.ResponseWriter) {
	writeJsonResponse(w, r, http.StatusOK)
}

func refreshSession(id int, ctx *apiContext) apiResponse {
	var info map[string]interface{}
	if err := ctx.body.Decode(&info); err != nil {
		log.Println("Invalid session refresh body:", err)
		return badRequestResponse("Unparseable JSON request body")
	}
	err := db.RefreshSession(info, id, ctx.updateKey, ctx.db)
	if err != nil {
		if _, isRefreshError := err.(db.RefreshError); isRefreshError {
			return notFoundResponse()
		} else {
			return internalServerError()
		}
	}

	log.Println(ctx.clientIP, "refreshed", id)
	return refreshResponse{"ok", ""}
}

func deleteSession(id int, ctx *apiContext) apiResponse {
	ok, err := db.DeleteSession(id, ctx.updateKey, ctx.db)

	if err != nil {
		return internalServerError()
	}
	if ok {
		log.Println(ctx.clientIP, "delisted", id)
		return noContentResponse{}
	} else {
		return notFoundResponse()
	}
}

