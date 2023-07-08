package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/drawpile"
	"github.com/drawpile/listserver/inclsrv"
	"github.com/drawpile/listserver/validation"
	"github.com/gorilla/mux"
)

var nameRe = regexp.MustCompile(`\A[a-z0-9_]+\z`)

func serverInfo(ctx apiContext) map[string]interface{} {
	readonly := len(ctx.cfg.Database) == 0
	return map[string]interface{}{
		"api_name":    "drawpile-session-list",
		"version":     "1.6",
		"name":        ctx.cfg.Name,
		"description": ctx.cfg.Description,
		"favicon":     ctx.cfg.Favicon,
		"source":      "https://github.com/drawpile/listserver/",
		"read_only":   readonly,
		"public":      ctx.cfg.Public,
		"private":     ctx.cfg.Roomcodes && !readonly,
	}
}

// Return info about this list server
func apiRootHandler(r *http.Request) http.Handler {
	ctx := r.Context().Value(apiCtxKey).(apiContext)
	return JsonResponseOk(serverInfo(ctx))
}

// Return the session list
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

// Announce a new session
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

// /
// / Batch refresh sessions
// /
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
	errors := make(map[string]interface{})
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
				errors[id] = err.Error()
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
		"errors":    errors,
	})
}

// Refresh a single announcement
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
			return ErrorResponse(err.Error(), http.StatusBadRequest)
		} else {
			log.Println("Session refresh error:", err)
			return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
		}
	}

	return JsonResponseOk(map[string]interface{}{
		"status": "ok",
	})
}

// Unlist a session
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

// Find a session by its roomcode
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

type adminSessionRequest struct {
	Ids          []int64 `json:"ids"`
	Unlisted     bool    `json:"unlisted"`
	UnlistReason string  `json:"unlistreason"`
}

func apiAdminSessionPutHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permSessions, accessManage) {
		return ErrorResponse("You're not allowed to edit sessions", http.StatusForbidden)
	}

	var info adminSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return ErrorResponse("Unparseable JSON request body", http.StatusBadRequest)
	} else if len(info.Ids) == 0 {
		return ErrorResponse("No ids given", http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	updated, err := ctx.db.AdminUpdateSessions(info.Ids, info.Unlisted, info.UnlistReason, r.Context())
	if err != nil {
		log.Println("Put session error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status":  "ok",
		"updated": updated,
	})
}

func apiAdminRootHandler(r *http.Request) http.Handler {
	apiCtx := r.Context().Value(apiCtxKey).(apiContext)
	adminCtx := r.Context().Value(adminCtxKey).(adminContext)
	return JsonResponseOk(map[string]interface{}{
		"server": serverInfo(apiCtx),
		"config": map[string]interface{}{
			"checkserver":             apiCtx.cfg.CheckServer,
			"maxsessionsperhost":      apiCtx.cfg.MaxSessionsPerHost,
			"maxsessionspernamedhost": apiCtx.cfg.MaxSessionsPerNamedHost,
			"protocolwhitelist":       apiCtx.cfg.ProtocolWhitelist,
			"sessiontimeout":          apiCtx.cfg.SessionTimeout,
		},
		"user": map[string]interface{}{
			"id":    adminCtx.userId,
			"name":  adminCtx.userName,
			"admin": adminCtx.admin,
			"permissions": map[string]int{
				"sessions": adminCtx.access[permSessions],
				"hostbans": adminCtx.access[permHostBans],
				"roles":    adminCtx.access[permRoles],
				"users":    adminCtx.access[permUsers],
			},
		},
	})
}

func apiAdminSessionListHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permSessions, accessView) {
		return ErrorResponse("You're not allowed to view sessions", http.StatusForbidden)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	sessions, err := ctx.db.AdminQuerySessions(r.Context())
	if err != nil {
		log.Println("List sessions error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	for i, urlString := range ctx.cfg.IncludeServers {
		serverSessions, err := inclsrv.FetchServerAdminSessionList(urlString)
		if err == nil {
			sessions = append(sessions, serverSessions...)
		} else {
			host := ""
			urlValue, urlParseErr := url.Parse(urlString)
			if urlParseErr == nil {
				host = urlValue.Hostname()
			}
			if host == "" {
				host = fmt.Sprintf("include server #%d", i)
			}
			sessions = append(sessions, db.AdminSession{
				Included: true,
				Host:     host,
				Error:    fmt.Sprintf("Error including sessions"),
			})
		}
	}

	return JsonResponseOk(sessions)
}

type adminHostBanRequest struct {
	Host    string `json:"host"`
	Expires string `json:"expires"`
	Notes   string `json:"notes"`
}

func parseAdminHostBanRequest(r *http.Request) (adminHostBanRequest, error) {
	var info adminHostBanRequest
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("Unparseable JSON request body")
	}

	info.Host = strings.TrimSpace(info.Host)
	if info.Host == "" {
		return info, fmt.Errorf("Host can't be blank")
	}

	if info.Expires != "" {
		if _, err := time.Parse("2006-01-02", info.Expires); err != nil {
			return info, fmt.Errorf("Expires has wrong format, should be YYYY-mm-dd")
		}
	}

	return info, nil
}

func apiAdminBanCreateHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permHostBans, accessManage) {
		return ErrorResponse("You're not allowed to create bans", http.StatusForbidden)
	}

	info, err := parseAdminHostBanRequest(r)
	if err != nil {
		return ErrorResponse(err.Error(), http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	id, err := ctx.db.AdminCreateHostBan(info.Host, info.Expires, info.Notes, r.Context())
	if err != nil {
		log.Println("Create host ban error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
		"id":     id,
	})
}

func apiAdminBanPutHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permHostBans, accessManage) {
		return ErrorResponse("You're not allowed to edit bans", http.StatusForbidden)
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return ErrorResponse("Invalid hostban id", http.StatusBadRequest)
	}

	info, err := parseAdminHostBanRequest(r)
	if err != nil {
		return ErrorResponse(err.Error(), http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	updated, err := ctx.db.AdminUpdateHostBan(id, info.Host, info.Expires, info.Notes, r.Context())
	if err != nil {
		log.Println("Put host ban error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !updated {
		return ErrorResponse("Hostban not found", http.StatusNotFound)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
	})
}

func apiAdminBanDeleteHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permHostBans, accessManage) {
		return ErrorResponse("You're not allowed to delete bans", http.StatusForbidden)
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return ErrorResponse("Invalid hostban id", http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	deleted, err := ctx.db.AdminDeleteHostBan(id, r.Context())
	if err != nil {
		log.Println("Delete host ban error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !deleted {
		return ErrorResponse("Hostban not found", http.StatusNotFound)
	}

	return JsonResponseOk(map[string]interface{}{
		"status": "ok",
	})
}

func apiAdminBanListHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permHostBans, accessView) {
		return ErrorResponse("You're not allowed to view bans", http.StatusForbidden)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	hostBans, err := ctx.db.AdminQueryHostBans(r.Context())
	if err != nil {
		log.Println("List host bans error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseOk(hostBans)
}

type adminRoleRequest struct {
	Name           string `json:"name"`
	Admin          bool   `json:"admin"`
	AccessSessions int    `json:"accesssessions"`
	AccessHostBans int    `json:"accesshostbans"`
	AccessRoles    int    `json:"accessroles"`
	AccessUsers    int    `json:"accessusers"`
}

func isValidAccess(access int) bool {
	return access == accessNone || access == accessView || access == accessManage
}

func isValidViewAccess(access int) bool {
	return access == accessNone || access == accessView
}

func parseAdminRoleRequest(r *http.Request) (adminRoleRequest, error) {
	var info adminRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("Unparseable JSON request body")
	}

	info.Name = strings.TrimSpace(info.Name)
	if info.Name == "" {
		return info, fmt.Errorf("Name can't be empty")
	} else if !nameRe.MatchString(info.Name) {
		return info, fmt.Errorf("Name must consist only of a-z, 0-9 and _")
	}

	accessOk := isValidAccess(info.AccessSessions) &&
		isValidAccess(info.AccessHostBans) &&
		isValidViewAccess(info.AccessRoles) &&
		isValidViewAccess(info.AccessUsers)
	if !accessOk {
		return info, fmt.Errorf("Invalid access values")
	}

	return info, nil
}

func apiAdminRoleCreateHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permRoles, accessManage) {
		return ErrorResponse("You're not allowed to create roles", http.StatusForbidden)
	}

	info, err := parseAdminRoleRequest(r)
	if err != nil {
		return ErrorResponse(err.Error(), http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	existingRole, err := ctx.db.AdminQueryRoleByName(info.Name, r.Context())
	if err != nil {
		log.Println("Create role exists query error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if existingRole.Id != 0 {
		return ErrorResponse("A role with that name already exists", http.StatusBadRequest)
	}

	id, err := ctx.db.AdminCreateRole(
		info.Name, info.Admin, int64(info.AccessSessions), int64(info.AccessHostBans),
		int64(info.AccessRoles), int64(info.AccessUsers), r.Context())
	if err != nil {
		log.Println("Create role error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
		"id":     id,
	})
}

func apiAdminRolePutHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permRoles, accessManage) {
		return ErrorResponse("You're not allowed to edit roles", http.StatusForbidden)
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return ErrorResponse("Invalid role id", http.StatusBadRequest)
	}

	info, err := parseAdminRoleRequest(r)
	if err != nil {
		return ErrorResponse(err.Error(), http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	existingRole, err := ctx.db.AdminQueryRoleByName(info.Name, r.Context())
	if err != nil {
		log.Println("Create role exists query error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if existingRole.Id != 0 && existingRole.Id != id {
		return ErrorResponse("A different role with that name already exists", http.StatusBadRequest)
	}

	updated, err := ctx.db.AdminUpdateRole(
		id, info.Name, info.Admin, int64(info.AccessSessions), int64(info.AccessHostBans),
		int64(info.AccessRoles), int64(info.AccessUsers), r.Context())
	if err != nil {
		log.Println("Put role error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !updated {
		return ErrorResponse("Role not found", http.StatusNotFound)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
	})
}

func apiAdminRoleDeleteHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permRoles, accessManage) {
		return ErrorResponse("You're not allowed to delete roles", http.StatusForbidden)
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return ErrorResponse("Invalid role id", http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	deleted, err := ctx.db.AdminDeleteRole(id, r.Context())
	if err != nil {
		log.Println("Delete role error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !deleted {
		return ErrorResponse("Role not found", http.StatusNotFound)
	}

	return JsonResponseOk(map[string]interface{}{
		"status": "ok",
	})
}

func apiAdminRoleListHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permRoles, accessView) {
		return ErrorResponse("You're not allowed to view roles", http.StatusForbidden)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	roles, err := ctx.db.AdminQueryRoles(r.Context())
	if err != nil {
		log.Println("List roles error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseOk(roles)
}

type adminUserRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
	RoleId   int64  `json:"-"`
}

func parseAdminUserRequest(r *http.Request, requirePassword bool) (adminUserRequest, error, int) {
	var info adminUserRequest
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("Unparseable JSON request body"), http.StatusBadRequest
	}

	info.Name = strings.TrimSpace(info.Name)
	if info.Name == "" {
		return info, fmt.Errorf("Name can't be empty"), http.StatusBadRequest
	} else if !nameRe.MatchString(info.Name) {
		return info, fmt.Errorf("Name must consist only of a-z, 0-9 and _"), http.StatusBadRequest
	}

	info.Password = strings.TrimSpace(info.Password)
	if requirePassword && info.Password == "" {
		return info, fmt.Errorf("Password can't be empty"), http.StatusBadRequest
	}

	info.Role = strings.TrimSpace(info.Role)
	if info.Role == "" {
		return info, fmt.Errorf("Role can't be empty"), http.StatusBadRequest
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	existingRole, err := ctx.db.AdminQueryRoleByName(info.Role, r.Context())
	if err != nil {
		log.Println("Create role exists query error:", err)
		return info, err, http.StatusInternalServerError
	} else if existingRole.Id == 0 {
		return info, fmt.Errorf("Role doesn't exist"), http.StatusBadRequest
	}
	info.RoleId = existingRole.Id

	return info, nil, http.StatusOK
}

func apiAdminUserCreateHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permUsers, accessManage) {
		return ErrorResponse("You're not allowed to create users", http.StatusForbidden)
	}

	info, err, status := parseAdminUserRequest(r, true)
	if err != nil {
		return ErrorResponse(err.Error(), status)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	existingUser, err := ctx.db.AdminQueryUserByName(info.Name, r.Context())
	if err != nil {
		log.Println("Create user exists query error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if existingUser.Id != 0 {
		return ErrorResponse("A user with that name already exists", http.StatusBadRequest)
	}

	passwordHash, err := hashPassword(info.Password)
	if err != nil {
		log.Println("Create user password hash error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	id, err := ctx.db.AdminCreateUser(
		info.Name, passwordHash, info.RoleId, r.Context())
	if err != nil {
		log.Println("Create user error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
		"id":     id,
	})
}

func apiAdminUserPutHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permUsers, accessManage) {
		return ErrorResponse("You're not allowed to edit users", http.StatusForbidden)
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return ErrorResponse("Invalid user id", http.StatusBadRequest)
	}

	info, err, status := parseAdminUserRequest(r, false)
	if err != nil {
		return ErrorResponse(err.Error(), status)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	existingUser, err := ctx.db.AdminQueryUserByName(info.Name, r.Context())
	if err != nil {
		log.Println("Edit user exists query error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if existingUser.Id != 0 && existingUser.Id != id {
		return ErrorResponse("A different user with that name already exists", http.StatusBadRequest)
	}

	passwordHash := ""
	if info.Password != "" {
		passwordHash, err = hashPassword(info.Password)
		if err != nil {
			log.Println("Edit user password hash error:", err)
			return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
		}
	}

	updated, err := ctx.db.AdminUpdateUser(
		id, info.Name, passwordHash, info.RoleId, r.Context())
	if err != nil {
		log.Println("Put user error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !updated {
		return ErrorResponse("User not found", http.StatusNotFound)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
	})
}

func apiAdminUserDeleteHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permUsers, accessManage) {
		return ErrorResponse("You're not allowed to delete users", http.StatusForbidden)
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return ErrorResponse("Invalid user id", http.StatusBadRequest)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	deleted, err := ctx.db.AdminDeleteUser(id, r.Context())
	if err != nil {
		log.Println("Delete user error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !deleted {
		return ErrorResponse("User not found", http.StatusNotFound)
	}

	return JsonResponseOk(map[string]interface{}{
		"status": "ok",
	})
}

func apiAdminUserListHandler(r *http.Request) http.Handler {
	if !adminAccess(r, permUsers, accessView) {
		return ErrorResponse("You're not allowed to view users", http.StatusForbidden)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)

	users, err := ctx.db.AdminQueryUsers(r.Context())
	if err != nil {
		log.Println("List users error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	return JsonResponseOk(users)
}

type adminChangePasswordRequest struct {
	Password string `json:"password"`
}

func parseAdminChangePasswordRequest(r *http.Request) (adminChangePasswordRequest, error) {
	var info adminChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("Unparseable JSON request body")
	}

	info.Password = strings.TrimSpace(info.Password)
	if info.Password == "" {
		return info, fmt.Errorf("Password can't be empty")
	}

	return info, nil
}

func apiAdminUserSelfPasswordPutHandler(r *http.Request) http.Handler {
	id := ownUserId(r)
	if id == 0 {
		return ErrorResponse("Admin password can't be changed", http.StatusForbidden)
	}

	info, err := parseAdminChangePasswordRequest(r)
	if err != nil {
		return ErrorResponse(err.Error(), http.StatusBadRequest)
	}

	passwordHash := ""
	passwordHash, err = hashPassword(info.Password)
	if err != nil {
		log.Println("Change password hash error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	updated, err := ctx.db.AdminUpdateUserPassword(
		id, passwordHash, r.Context())
	if err != nil {
		log.Println("Change password error:", err)
		return ErrorResponse("An internal error occurred", http.StatusInternalServerError)
	} else if !updated {
		return ErrorResponse("User not found", http.StatusNotFound)
	}

	return JsonResponseCreated(map[string]interface{}{
		"status": "ok",
	})
}
