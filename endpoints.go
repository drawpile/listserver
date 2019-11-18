package main

import (
	"encoding/json"
	"fmt"
	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/drawpile"
	"github.com/drawpile/listserver/validation"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

/**
 * API root: info about this server
 */
type apiRootResponse struct {
	ApiName     string `json:"api_name"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Favicon     string `json:"favicon,omitempty"`
	Source      string `json:"source"`
	Public      bool   `json:"public"`
	Private     bool   `json:"private"`
}

func (r apiRootResponse) WriteResponse(w http.ResponseWriter) {
	writeJsonResponse(w, r, http.StatusOK)
}

func RootEndpoint(ctx *apiContext) apiResponse {
	if len(ctx.path) != 0 {
		return notFoundResponse()
	}
	if ctx.method != "GET" {
		return methodNotAllowedResponse{"GET"}
	}

	return apiRootResponse{
		ApiName:     "drawpile-session-list",
		Version:     "1.6",
		Name:        ctx.cfg.Name,
		Description: ctx.cfg.Description,
		Favicon:     ctx.cfg.Favicon,
		Source:      "https://github.com/drawpile/listserver/",
		Public:      ctx.cfg.Public,
		Private:     ctx.cfg.Roomcodes,
	}
}

/**
 * List and edit sessions
 */

type SessionListResponse struct {
	sessions      []db.SessionInfo
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
	if !ctx.cfg.Public {
		return forbiddenResponse()
	}
	if len(ctx.path) == 0 {
		if ctx.method == "GET" {
			return getSessionList(ctx)

		} else if ctx.method == "POST" {
			return postNewSession(ctx)

		} else if ctx.method == "PUT" {
			return batchRefreshSessions(ctx)

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
		Title:    ctx.query.Get("title"),
		Nsfm:     ctx.query.Get("nsfm") == "true",
		Protocol: ctx.query.Get("protocol"),
	}
	list, err := db.QuerySessionList(opts, ctx.db)
	if err != nil {
		return internalServerError()
	}
	return SessionListResponse{list, ctx.query.Get("callback")}
}

type SessionJoinResponse struct {
	db.JoinSessionInfo
}

func (r SessionJoinResponse) WriteResponse(w http.ResponseWriter) {
	writeJsonResponse(w, r, http.StatusOK)
}

func RoomCodeEndpoint(ctx *apiContext) apiResponse {
	if !ctx.cfg.Roomcodes {
		return forbiddenResponse()
	}

	code := strings.TrimSuffix(ctx.path, "/")
	if len(code) != 5 {
		return notFoundResponse()
	}
	for _, r := range code {
		if !unicode.IsLetter(r) {
			return notFoundResponse()
		}
	}

	if ctx.method == "GET" {
		session, err := db.QuerySessionByRoomcode(code, ctx.db)
		if err != nil {
			return internalServerError()
		}
		if session.Host == "" {
			return notFoundResponse()
		}
		return SessionJoinResponse{session}

	} else {
		return methodNotAllowedResponse{"GET"}
	}
}

type announcementResponse struct {
	*db.NewSessionInfo
	Expires int    `json:"expires"`
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

	// Check if public / private listing is enabled
	if (info.Private && !ctx.cfg.Roomcodes) || (!info.Private && !ctx.cfg.Public) {
		return forbiddenResponse()
	}

	// Validate
	rules := validation.AnnouncementValidationRules{
		ClientIP:            ctx.clientIP,
		AllowWellKnownPorts: ctx.cfg.AllowWellKnownPorts,
		ProtocolWhitelist:   ctx.cfg.ProtocolWhitelist,
	}
	if err := validation.ValidateAnnouncement(info, rules); err != nil {
		if _, isValidationError := err.(validation.ValidationError); isValidationError {
			log.Println(ctx.clientIP, "invalid announcement:", err)
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

	info.Nsfm = info.Nsfm || ctx.cfg.ContainsNsfmWords(info.Title)

	// Make sure this host isn't banned
	if ctx.cfg.IsBannedHost(info.Host) {
		return forbiddenResponse()
	}

	// Make sure this hasn't been announced yet
	if isActive, err := db.IsActiveSession(info.Host, info.Id, info.Port, ctx.db); err != nil {
		return internalServerError()
	} else if isActive {
		log.Println(ctx.clientIP, "tried to relist session", info.Id)
		return conflictResponse("Session already listed")
	}

	// Check per-host session limit
	if !ctx.cfg.IsTrustedHost(info.Host) {
		var maxSessions int
		if validation.IsNamedHost(info.Host) {
			maxSessions = ctx.cfg.MaxSessionsPerNamedHost
		} else {
			maxSessions = ctx.cfg.MaxSessionsPerHost
		}

		if count, err := db.GetHostSessionCount(info.Host, ctx.db); err != nil {
			return internalServerError()
		} else if count >= maxSessions {
			log.Println(ctx.clientIP, "exceeded max. announcement count")
			return conflictResponse("Max listing count exceeded for this host")
		}

		// Do a connectivity check only for hosts that use an IP address
		// because we can assume that anyone who has gone through the trouble
		// of configuring a domain name can figure out connectivity problems without
		// the list server's help.
		if ctx.cfg.CheckServer && !validation.IsNamedHost(info.Host) {
			if err := drawpile.TryDrawpileLogin(info.HostAddress(), info.Protocol); err != nil {
				log.Println(info.HostAddress(), "does not seem to be running a Drawpile server")
				return conflictResponse(err.Error())
			}
		}
	}

	// Insert to database
	newses, err := db.InsertSession(info, ctx.clientIP.String(), ctx.db)
	if err != nil {
		return internalServerError()
	}

	// Assign room code
	if ctx.cfg.Roomcodes {
		roomcode, err := db.AssignRoomcode(newses.ListingId, ctx.db)
		if err != nil {
			log.Println("Warning: couldn't assign roomcode to listing", newses.ListingId, err)
		} else {
			newses.Roomcode = roomcode
		}
	}

	var announcementType string
	if newses.Private {
		announcementType = "private"
	} else {
		announcementType = "public"
	}

	log.Println(ctx.clientIP, "announced", announcementType, "session", newses.ListingId, info.Host, info.Port, info.Id)

	// Add a warning message if hostname is an IPv6 address
	welcomeMsg := ctx.cfg.Welcome

	if ctx.cfg.WarnIpv6 && validation.IsIpv6Address(info.Host) {
		welcomeMsg = welcomeMsg + "\nNote: your host address is an IPv6 address. It may not be accessible by all users."
	}

	return announcementResponse{
		&newses,
		db.SessionTimeout,
		welcomeMsg,
	}
}

type refreshResponse struct {
	Status    string                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Responses map[string]interface{} `json:"responses,omitempty"`
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
	return refreshResponse{"ok", "", nil}
}

func batchRefreshSessions(ctx *apiContext) apiResponse {
	var refreshBatch map[string]interface{}
	if err := ctx.body.Decode(&refreshBatch); err != nil {
		log.Println("Invalid batch refresh body:", err)
		return badRequestResponse("Unparseable JSON request body")
	}

	if len(refreshBatch) == 0 {
		return badRequestResponse("At least one session should be included")
	}

	responses := make(map[string]interface{})
	idlist := make([]string, 0, len(refreshBatch))

	for id, info := range refreshBatch {
		sessionId, err := strconv.Atoi(id)
		if err != nil {
			return badRequestResponse(id + " is not an integer!")
		}

		sessionInfo, ok := info.(map[string]interface{})
		if !ok {
			return badRequestResponse(id + ": expected object")
		}

		updateKey, ok := sessionInfo["updatekey"].(string)
		if !ok {
			return badRequestResponse(id + ".updatekey: expected string")
		}

		err = db.RefreshSession(sessionInfo, sessionId, updateKey, ctx.db)
		if err != nil {
			if _, isRefreshError := err.(db.RefreshError); isRefreshError {
				responses[id] = "error"
			} else {
				return internalServerError()
			}
		} else {
			responses[id] = "ok"
			idlist = append(idlist, id)
		}
	}

	log.Println(ctx.clientIP, "batch refreshed", idlist)
	return refreshResponse{"ok", "", responses}
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
