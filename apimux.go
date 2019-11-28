package main

import (
	"encoding/json"
	"fmt"
	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/ratelimit"
	"github.com/patrickmn/go-cache"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// An extended ServeMux for the API endpoints
type apiMux struct {
	*http.ServeMux
	cfg         *config
	db          db.Database
	cache       *cache.Cache
	ratelimiter *ratelimit.BucketMap
}

// API request context
type apiContext struct {
	path      string
	method    string
	query     url.Values
	header    *http.Header
	body      *json.Decoder
	clientIP  net.IP
	updateKey string
	cfg       *config
	db        db.Database
	cache     *cache.Cache
}

func getRemoteAddr(header string, req *http.Request) (remoteAddr net.IP) {
	if len(header) > 0 {
		realip := req.Header.Get(header)
		if len(realip) > 0 {
			remoteAddr = net.ParseIP(realip)
			if remoteAddr == nil {
				log.Println("Invalid", header, "remote address:", realip)
			}
		} else {
			log.Println(header, "header not set!")
		}
	} else {
		sep := strings.LastIndex(req.RemoteAddr, ":")
		remoteAddr = net.ParseIP(req.RemoteAddr[0:sep])
		if remoteAddr == nil {
			log.Println("Invalid remote address:", req.RemoteAddr[0:sep])
		}
	}
	return
}

func (mux *apiMux) HandleApiEndpoint(prefix string, endPoint func(*apiContext) apiResponse) {
	mux.HandleFunc(prefix, func(w http.ResponseWriter, req *http.Request) {
		if p := strings.TrimPrefix(req.URL.Path, prefix); len(p) < len(req.URL.Path) {
			remoteAddr := getRemoteAddr(mux.cfg.RemoteAddressHeader, req)
			if remoteAddr == nil {
				// Misconfiguration: remote IP address not available
				internalServerError().WriteResponse(w)

			} else {
				if req.Method != "GET" {
					remoteAddrString := remoteAddr.String()
					// Request rate limiting
					if !mux.ratelimiter.AddToken(remoteAddrString) {
						genericErrorResponse{
							http.StatusTooManyRequests,
							fmt.Sprintf("Too many requests. Wait %d seconds.", mux.ratelimiter.DrainTime(remoteAddrString)),
						}.WriteResponse(w)
						return
					}

					// User agent check
					if mux.cfg.CheckUserAgent {
						if !strings.HasPrefix(req.Header.Get("User-Agent"), "DrawpileListingClient/") {
							forbiddenResponse().WriteResponse(w)
							return
						}
					}
				}

				// Build request context
				ctx := apiContext{
					path:      p,
					method:    req.Method,
					query:     url.Values{},
					header:    &req.Header,
					body:      json.NewDecoder(req.Body),
					clientIP:  remoteAddr,
					updateKey: req.Header.Get("X-Update-Key"),
					cfg:       mux.cfg,
					db:        mux.db,
					cache:     mux.cache,
				}

				if req.Method == "GET" {
					if err := req.ParseForm(); err != nil {
						badRequestResponse(err.Error()).WriteResponse(w)
						return
					}
					ctx.query = req.Form
				}

				// Call API endpoint
				endPoint(&ctx).WriteResponse(w)
			}

		} else {
			notFoundResponse().WriteResponse(w)
		}
	})
}
