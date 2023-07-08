package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

const (
	permSessions = 0
	permHostBans = 1
	permRoles    = 2
	permUsers    = 3
	permCount    = 4
	accessNone   = 0
	accessView   = 1
	accessManage = 2
)

type adminAuthMiddleware struct {
	adminUser string
	adminPass string
}

type adminContext struct {
	userId   int64
	userName string
	admin    bool
	access   [permCount]int
}

func (aam *adminAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, err, adminctx := aam.checkAdminCredentials(r); ok {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminCtxKey, adminctx)))
		} else if err != nil {
			log.Println("Admin auth error:", err)
			ErrorResponse("An internal error occurred", http.StatusInternalServerError).ServeHTTP(w, r)
		} else {
			w.Header().Add("WWW-Authenticate", `Basic realm="list server administration"`)
			ErrorResponse("Unauthorized", http.StatusUnauthorized).ServeHTTP(w, r)
		}
	})
}

func (aam *adminAuthMiddleware) checkAdminCredentials(r *http.Request) (bool, error, adminContext) {
	username, password, ok := r.BasicAuth()
	if !ok || username == "" || password == "" {
		return false, nil, adminContext{}
	}

	if aam.adminUser != "" && aam.adminPass != "" && username == aam.adminUser && password == aam.adminPass {
		return true, nil, adminContext{
			userId:   0,
			userName: aam.adminUser,
			admin:    true,
		}
	}

	ctx := r.Context().Value(apiCtxKey).(apiContext)
	user, err := ctx.db.AdminQueryUserByName(username, r.Context())
	if err != nil || user.Id == 0 || !checkPassword(password, user.PasswordHash) {
		return false, err, adminContext{}
	}

	return true, nil, adminContext{
		userId:   user.Id,
		userName: user.Name,
		admin:    user.Role.Admin,
		access: [permCount]int{
			permSessions: user.Role.AccessSessions,
			permHostBans: user.Role.AccessHostBans,
			permRoles:    user.Role.AccessRoles,
			permUsers:    user.Role.AccessUsers,
		},
	}
}

func checkPassword(password string, passwordHash string) bool {
	decoded, err := base64.RawStdEncoding.DecodeString(passwordHash)
	if err != nil {
		log.Println("Error decoding password hash:", err)
		return false
	}
	return bcrypt.CompareHashAndPassword(decoded, []byte(password)) == nil
}

func hashPassword(password string) (string, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(passwordHash), nil
}

func clampAccess(ctx *adminContext, perm int) int {
	access := ctx.access[perm]
	if perm == permRoles || perm == permUsers {
		if access >= accessView {
			return accessView
		} else {
			return accessNone
		}
	} else {
		return access
	}
}

func adminAccess(r *http.Request, perm int, access int) bool {
	ctx := r.Context().Value(adminCtxKey).(adminContext)
	return ctx.admin || clampAccess(&ctx, perm) >= access
}

func ownUserId(r *http.Request) int64 {
	return r.Context().Value(adminCtxKey).(adminContext).userId
}
