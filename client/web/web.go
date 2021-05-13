package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
	"github.com/sgielen/rufs/config"
)

func Init(port int) {
	m := http.NewServeMux()
	m.Handle("/api/config", convreq.Wrap(renderConfig, convreq.WithErrorHandler(errorHandler)))
	m.Handle("/api/register", convreq.Wrap(registerCircle, convreq.WithErrorHandler(errorHandler)))
	m.Handle("/api/shares_in_circle", convreq.Wrap(sharesInCircle, convreq.WithErrorHandler(errorHandler)))
	m.Handle("/api/add_share", convreq.Wrap(addShare, convreq.WithErrorHandler(errorHandler)))
	m.Handle("/api/open_explorer", convreq.Wrap(openExplorer, convreq.WithErrorHandler(errorHandler)))
	m.Handle("/", convreq.Wrap(renderStatic))
	http.HandleFunc("/", authMiddleWare(m))
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatalf("Failed to start HTTP server on port %d: %v", port, err)
	}
}

func errorHandler(code int, msg string, r *http.Request) convreq.HttpResponse {
	return errorResponse(code, msg)
}

func errorResponse(code int, msg string) convreq.HttpResponse {
	b, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		b, err = json.Marshal(map[string]string{"error": "JSON encode failed: " + err.Error()})
		if err != nil {
			b = []byte(`{"error": "Well, shit."}`)
		}
	}
	return respond.OverrideResponseCode(respond.WithHeader(respond.Bytes(b), "Content-Type", "text/json"), code)
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
			errorResponse(403, "Request from "+r.RemoteAddr+" denied").Respond(w, r)
			return
		}
		h.ServeHTTP(w, r)
	}
}

func respondJSON(v interface{}) convreq.HttpResponse {
	b, err := json.Marshal(v)
	if err != nil {
		return respond.Error(fmt.Errorf("JSON encode failed: %v", err))
	}
	return respond.WithHeader(respond.Bytes(b), "Content-Type", "text/json")
}

func renderConfig(ctx context.Context, req *http.Request) convreq.HttpResponse {
	ret := config.Config{
		Circles: config.GetCircles(),
	}
	return respondJSON(ret)
}

type registerCircleGet struct {
	User   string `schema:"user,required"`
	Token  string `schema:"token,required"`
	Circle string `schema:"circle,required"`
	Ca     string `schema:"ca,required"`
}

func registerCircle(ctx context.Context, req *http.Request, get registerCircleGet) convreq.HttpResponse {
	return respond.InternalServerError("not yet implemented")
}

type sharesInCircleGet struct {
	Circle string `schema:"circle,required"`
}

func sharesInCircle(ctx context.Context, req *http.Request, get sharesInCircleGet) convreq.HttpResponse {
	return respond.InternalServerError("not yet implemented")
}

type addShareGet struct {
	Circle string `schema:"circle,required"`
	Share  string `schema:"share,required"`
	Local  string `schema:"local,required"`
}

func addShare(ctx context.Context, req *http.Request, get addShareGet) convreq.HttpResponse {
	return respond.InternalServerError("not yet implemented")
}

func openExplorer(ctx context.Context, req *http.Request) convreq.HttpResponse {
	return respond.InternalServerError("not yet implemented")
}

func renderStatic(ctx context.Context, req *http.Request) convreq.HttpResponse {
	path := req.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	if body, ok := staticFiles[path]; ok {
		t := "application/octet-stream"
		if strings.HasSuffix(path, ".html") {
			t = "text/html"
		} else if strings.HasSuffix(path, ".js") {
			t = "application/javascript"
		}
		return respond.WithHeader(respond.String(body), "Content-Type", t)
	} else {
		return respond.NotFound("File not found")
	}
}
