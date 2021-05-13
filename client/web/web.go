package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
	"github.com/sgielen/rufs/config"
)

func Init(port int) {
	m := http.NewServeMux()
	m.Handle("/api/config", convreq.Wrap(renderConfig))
	http.HandleFunc("/", authMiddleWare(m))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatalf("Failed to start HTTP server on port %d: %v", port, err)
	}
}

func checkRemoteAddr(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		h = addr
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func authMiddleWare(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkRemoteAddr(r.RemoteAddr) {
			w.WriteHeader(403)
			fmt.Fprintf(w, `{"error": "Request from `+r.RemoteAddr+` denied"}`)
			return
		}
		h.ServeHTTP(w, r)
	}
}

func respondJSON(v interface{}) convreq.HttpResponse {
	code := 200
	b, err := json.Marshal(v)
	if err != nil {
		code = 500
		b, err = json.Marshal(map[string]string{"error": "JSON encode failed: " + err.Error()})
		if err != nil {
			b = []byte(`{"error": "Well, shit."}`)
		}
	}
	return respond.OverrideResponseCode(respond.WithHeader(respond.String(string(b)), "Content-Type", "text/json"), code)
}

func renderConfig(ctx context.Context, req *http.Request) convreq.HttpResponse {
	ret := config.Config{
		Circles: config.GetCircles(),
	}
	return respondJSON(ret)
}
