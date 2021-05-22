package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
	"github.com/pkg/browser"
	"github.com/sgielen/rufs/client/config"
	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/register"
	"github.com/sgielen/rufs/client/shares"
	"github.com/sgielen/rufs/client/vfs"
	"github.com/sgielen/rufs/version"

	// Register /debug/ HTTP handlers.
	_ "github.com/sgielen/rufs/debugging"
)

var (
	ReloadConfigCallback func()
)

func Init(addr string) {
	http.Handle("/api/version", convreq.Wrap(renderVersion, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/config", convreq.Wrap(renderConfig, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/register", convreq.Wrap(registerCircle, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/shares_in_circle", convreq.Wrap(sharesInCircle, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/add_share", convreq.Wrap(addShare, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/set_mountpoint", convreq.Wrap(setMountpoint, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/open_explorer", convreq.Wrap(openExplorer, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/", convreq.Wrap(renderStatic))
	log.Printf("web server listening on addr %s.", addr)
	if err := http.ListenAndServe(addr, authMiddleWare(http.DefaultServeMux)); err != nil {
		log.Fatalf("failed to start HTTP server on address %q: %v", addr, err)
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

func renderVersion(ctx context.Context, req *http.Request) convreq.HttpResponse {
	type Res struct {
		Version string
	}
	return respondJSON(Res{version.GetVersion()})
}

func renderConfig(ctx context.Context, req *http.Request) convreq.HttpResponse {
	return respondJSON(config.GetConfig())
}

type registerCircleGet struct {
	User   string `schema:"user,required"`
	Token  string `schema:"token,required"`
	Circle string `schema:"circle,required"`
	Ca     string `schema:"ca,required"`
}

func registerCircle(ctx context.Context, req *http.Request, get registerCircleGet) convreq.HttpResponse {
	if err := register.Register(ctx, get.Circle, get.User, get.Token, get.Ca); err != nil {
		return respond.Error(err)
	}
	if err := config.AddCircleAndStore(get.Circle); err != nil {
		return respond.Error(err)
	}
	ReloadConfigCallback()
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := connectivity.WaitForCircle(wctx, get.Circle); err != nil && err != context.DeadlineExceeded {
		return respond.Error(err)
	}
	return respondJSON(map[string]bool{"ok": true})
}

type sharesInCircleGet struct {
	Circle string `schema:"circle,required"`
}

func sharesInCircle(ctx context.Context, req *http.Request, get sharesInCircleGet) convreq.HttpResponse {
	type Res struct {
		Shares []string
	}
	res := Res{}
	// TODO(quis): Only readdir get.Circle.
	dir := vfs.Readdir(ctx, "")
	for fn, fi := range dir.Files {
		if fi.IsDirectory {
			res.Shares = append(res.Shares, fn)
		}
	}
	return respondJSON(res)
}

type addShareGet struct {
	Circle string `schema:"circle,required"`
	Share  string `schema:"share,required"`
	Local  string `schema:"local,required"`
}

func addShare(ctx context.Context, req *http.Request, get addShareGet) convreq.HttpResponse {
	if err := config.AddShareAndStore(get.Circle, get.Share, get.Local); err != nil {
		return respond.Error(err)
	}
	if err := shares.ReloadConfig(); err != nil {
		return respond.Error(err)
	}
	return respondJSON(map[string]bool{"ok": true})
}

type setMountpointGet struct {
	Mountpoint string `schema:"mountpoint,required"`
}

func setMountpoint(ctx context.Context, req *http.Request, get setMountpointGet) convreq.HttpResponse {
	if err := config.SetMountpointAndStore(get.Mountpoint); err != nil {
		return respond.Error(err)
	}
	config.SetMountpoint(get.Mountpoint)
	return respondJSON(map[string]bool{"ok": true})
}

func openExplorer(ctx context.Context, req *http.Request) convreq.HttpResponse {
	mp := config.GetMountpoint()
	if mp == "" {
		return respond.BadRequest("no mountpoint configured")
	}
	browser.OpenURL(mp)
	return respondJSON(map[string]bool{"ok": true})
}

func renderStatic(ctx context.Context, req *http.Request) convreq.HttpResponse {
	path := req.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	body, ok := staticFiles[path]
	if !ok {
		return respond.NotFound("File not found")
	}

	t := "application/octet-stream"
	if strings.HasSuffix(path, ".html") {
		t = "text/html"
	} else if strings.HasSuffix(path, ".css") {
		t = "text/css"
	} else if strings.HasSuffix(path, ".js") {
		t = "application/javascript"
	} else if strings.HasSuffix(path, ".svg") {
		t = "image/svg+xml"
	}
	return respond.WithHeader(respond.String(body), "Content-Type", t)
}
