package main

import (
	"encoding/json"
	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/drawpile"
	"github.com/drawpile/listserver/inclsrv"
	"github.com/drawpile/listserver/validation"
	"github.com/gorilla/mux"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
)

//
// Return info about this list server
//
func apiRootHandler(r *http.Request) http.Handler {
	ctx := r.Context().Value(apiCtxKey).(apiContext)
	readonly := len(ctx.cfg.Database) == 0

	return JsonResponseOk(map[string]interface{}{
		"api_name":    "drawpile-session-list",
		"version":     "1.6",
		"name":        ctx.cfg.Name,
		"description": ctx.cfg.Description,
		"favicon":     ctx.cfg.Favicon,
		"source":      "https://github.com/drawpile/listserver/",
		"read_only":   readonly,
		"public":      ctx.cfg.Public,
		"private":     ctx.cfg.Roomcodes && !readonly,
	})
}

//
// Return the session list
//
func apiSessionListHandler(r *http.Request) http.Handler {
	ctx := r.Context().Value(apiCtxKey).(apiContext)
	if !ctx.cfg.Public {
		return ErrorResponse("No public listings on this server", http.StatusNotFound)
	}

	if err := r.ParseForm(); err != nil {
		return ErrorResponse("Bad request", http.StatusBadRequest)
	}

	opts := db.QueryOptions{
		Title:    r.Form.Get("title"),
		Nsfm:     r.Form.Get("nsfm") == "true",
		Protocol: r.Form.Get("protocol"),
	}

	var list []db.SessionInfo
	var err error
	if ctx.db != nil {
		list, err = ctx.db.QuerySessionList(opts, r.Context())
		if err != nil {
			log.Println("Session list query error:", err)
			return ErrorResponse("An error occurred while querying session list", http.StatusInternalServerError)
		}
	}

	if len(ctx.cfg.IncludeServers) > 0 {
		list = inclsrv.MergeLists(
			list,
			inclsrv.FetchFilteredSessionLists(opts, ctx.cfg.IncludeServers...),
		)
	}

	if list == nil {
		log.Println("Neither database nor included servers configured!")
		return ErrorResponse("Server is misconfigured", http.StatusInternalServerError)
	}

	return JsonResponseOk(list)
}

//
// Announce a new session
//
func apiAnnounceSessionHandler(r *http.Request) http.Handler {
	clientIP := parseIp(r.RemoteAddr)

	if clientIP.IsUnspecified() {
		log.Println("Couldn't parse IP address:", r.RemoteAddr)
		return ErrorResponse("Server is misconfigured", http.StatusInternalServerError)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	var info db.SessionInfo
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return ErrorResponse("Unparseable JSON request body", http.StatusBadRequest)
	}

	// Check if listings are enabled
	if info.Private && !ctx.cfg.Roomcodes {
		return ErrorResponse("Private listings not enabled on this server", http.StatusNotFound)
	}
	if !info.Private && !ctx.cfg.Public {
		return ErrorResponse("Public listings not enabled on this server", http.StatusNotFound)
	}

	// Validate announcement
	rules := validation.AnnouncementValidationRules{
		ClientIP:            clientIP,
		AllowWellKnownPorts: ctx.cfg.AllowWellKnownPorts,
		ProtocolWhitelist:   ctx.cfg.ProtocolWhitelist,
	}

	if err := validation.ValidateAnnouncement(info, rules); err != nil {
		if _, isValidationError := err.(validation.ValidationError); isValidationError {
			return ErrorResponse(err.Error(), http.StatusBadRequest)
		} else {
			return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
		}
	}

	// Fill in default values
	if len(info.Host) == 0 {
		info.Host = clientIP.String()
	}
	if info.Port == 0 {
		info.Port = 27750
	}

	info.Nsfm = info.Nsfm || ctx.cfg.ContainsNsfmWords(info.Title)

	// Make sure this host isn't banned
	if banned, err := ctx.db.IsBannedHost(info.Host, r.Context()); banned || validation.IsHostInList(info.Host, ctx.cfg.BannedHosts) {
		return ErrorResponse("This host is not allowed to announce here", http.StatusForbidden)
	} else if err != nil {
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	// Make sure this hasn't been announced yet
	if isActive, err := ctx.db.IsActiveSession(info.Host, info.Id, info.Port, r.Context()); err != nil {
		log.Println("IsActive check error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)

	} else if isActive {
		return ErrorResponse("Session already listed", http.StatusBadRequest)
	}

	// Check per-host session limit
	if !validation.IsHostInList(info.Host, ctx.cfg.TrustedHosts) {
		var maxSessions int
		if validation.IsNamedHost(info.Host) {
			maxSessions = ctx.cfg.MaxSessionsPerNamedHost
		} else {
			maxSessions = ctx.cfg.MaxSessionsPerHost
		}

		if count, err := ctx.db.GetHostSessionCount(info.Host, r.Context()); err != nil {
			return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
		} else if count >= maxSessions {
			return ErrorResponse("Max listing count exceeded for this host", http.StatusBadRequest)
		}

		// Do a connectivity check only for hosts that use an IP address
		// because we can assume that anyone who has gone through the trouble
		// of configuring a domain name can figure out connectivity problems without
		// the list server's help.
		if ctx.cfg.CheckServer && !validation.IsNamedHost(info.Host) {
			if err := drawpile.TryDrawpileLogin(info.HostAddress(), info.Protocol); err != nil {
				log.Println(info.HostAddress(), "does not seem to be running a Drawpile server")
				return ErrorResponse(err.Error(), http.StatusBadRequest)
			}
		}
	}

	// Insert to database
	newses, err := ctx.db.InsertSession(info, clientIP.String(), r.Context())
	if err != nil {
		log.Println("Session insertion error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	// Assign room code
	if ctx.cfg.Roomcodes {
		roomcode, err := ctx.db.AssignRoomCode(newses.ListingId, r.Context())
		if err != nil {
			log.Println("Warning: couldn't assign roomcode to listing", newses.ListingId, err)
		} else {
			newses.Roomcode = roomcode
		}
	}

	// Add a warning message if hostname is an IPv6 address
	welcomeMsg := ctx.cfg.Welcome

	if ctx.cfg.WarnIpv6 && validation.IsIpv6Address(info.Host) {
		welcomeMsg = welcomeMsg + "\nNote: your host address is an IPv6 address. It may not be accessible by all users."
	}

	return JsonResponseOk(announcementResponse{
		&newses,
		ctx.db.SessionTimeoutMinutes(),
		welcomeMsg,
	})
}

func parseIp(addr string) (remoteAddr net.IP) {
	remoteAddr = net.ParseIP(addr)

	if remoteAddr == nil {
		// Remote address may be in format IP:port
		sep := strings.LastIndex(addr, ":")
		remoteAddr = net.ParseIP(addr[0:sep])
	}
	return
}

type announcementResponse struct {
	*db.NewSessionInfo
	Expires int    `json:"expires"`
	Message string `json:"message,omitempty"`
}

///
/// Batch refresh sessions
///
func apiBatchRefreshHandler(r *http.Request) http.Handler {
	ctx := r.Context().Value(apiCtxKey).(apiContext)

	var refreshBatch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&refreshBatch); err != nil {
		return ErrorResponse("Unparseable JSON request body", http.StatusBadRequest)
	}

	if len(refreshBatch) == 0 {
		return ErrorResponse("At least one session should be included", http.StatusBadRequest)
	}

	responses := make(map[string]interface{})
	idlist := make([]string, 0, len(refreshBatch))

	for id, info := range refreshBatch {
		sessionId, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return ErrorResponse(id+" is not an integer", http.StatusBadRequest)
		}

		sessionInfo, ok := info.(map[string]interface{})
		if !ok {
			return ErrorResponse(id+": expected object", http.StatusBadRequest)
		}

		updateKey, ok := sessionInfo["updatekey"].(string)
		if !ok {
			return ErrorResponse(id+".updatekey: expected string", http.StatusBadRequest)
		}

		err = ctx.db.RefreshSession(sessionInfo, sessionId, updateKey, r.Context())
		if err != nil {
			if _, isRefreshError := err.(db.RefreshError); isRefreshError {
				responses[id] = "error"
			} else {
				log.Println("Session batch refresh error:", err)
				return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
			}
		} else {
			responses[id] = "ok"
			idlist = append(idlist, id)
		}
	}

	return JsonResponseOk(map[string]interface{}{
		"status":    "ok",
		"responses": responses,
	})
}

//
// Refresh a single announcement
//
func apiRefreshHandler(r *http.Request) http.Handler {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		panic(err)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	var info map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return ErrorResponse("Unparseable JSON request body", http.StatusBadRequest)
	}

	err = ctx.db.RefreshSession(info, id, r.Header.Get("X-Update-Key"), r.Context())
	if err != nil {
		if _, isRefreshError := err.(db.RefreshError); isRefreshError {
			return ErrorResponse("No such session", http.StatusNotFound)
		} else {
			log.Println("Session refresh error:", err)
			return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
		}
	}

	return JsonResponseOk(map[string]interface{}{
		"status": "ok",
	})
}

//
// Unlist a session
//
func apiUnlistHandler(r *http.Request) http.Handler {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		panic(err)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	ok, err := ctx.db.DeleteSession(id, r.Header.Get("X-Update-Key"), r.Context())

	if err != nil {
		log.Println("Session deletion error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	if ok {
		return JsonResponseOk(map[string]string{
			"status": "ok",
		})
	} else {
		return ErrorResponse("No such session", http.StatusNotFound)
	}
}

//
// Find a session by its roomcode
//
func apiRoomCodeHandler(r *http.Request) http.Handler {
	ctx := r.Context().Value(apiCtxKey).(apiContext)

	if !ctx.cfg.Roomcodes {
		return ErrorResponse("No room codes on this server", http.StatusNotFound)
	}

	session, err := ctx.db.QuerySessionByRoomcode(mux.Vars(r)["code"], r.Context())
	if err != nil {
		log.Println("Roomcode query error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}
	if session.Host == "" {
		return ErrorResponse("No such session", http.StatusNotFound)
	}

	return JsonResponseOk(session)
}
