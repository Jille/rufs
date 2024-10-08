package web

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
	"github.com/Jille/rpcz"
	"github.com/Jille/rufs/client/config"
	"github.com/Jille/rufs/client/connectivity"
	"github.com/Jille/rufs/client/register"
	"github.com/Jille/rufs/client/shares"
	"github.com/Jille/rufs/client/vfs"
	"github.com/Jille/rufs/version"
	"github.com/pkg/browser"

	// Register /debug/ HTTP handlers.
	_ "github.com/Jille/rufs/debugging"
)

var (
	allowHosts   = flag.String("http_allow_hosts", "127.0.0.1/8, ::1/128", "Comma separated list of CIDRs to allow access to the web interface")
	passwordFile = flag.String("http_password_file", "", "File with the password for the web interface")

	acls     []*net.IPNet
	password string

	ReloadConfigCallback func()

	//go:embed dist
	staticFiles embed.FS
)

func Init(addr string) {
	if *passwordFile != "" {
		b, err := ioutil.ReadFile(*passwordFile)
		if err != nil {
			log.Fatalf("Failed to read password file %q: %v", *passwordFile, err)
		}
		password = strings.TrimSpace(string(b))
	}
	var err error
	acls, err = parseACLs(*allowHosts)
	if err != nil {
		log.Fatalf("Failed to parse --http_allow_hosts %q: %v", *allowHosts, err)
	}

	http.Handle("/api/version", convreq.Wrap(renderVersion, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/hostname", convreq.Wrap(renderHostname, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/config", convreq.Wrap(renderConfig, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/register", convreq.Wrap(registerCircle, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/shares_in_circle", convreq.Wrap(sharesInCircle, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/add_share", convreq.Wrap(addShare, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/set_mountpoint", convreq.Wrap(setMountpoint, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/api/open_explorer", convreq.Wrap(openExplorer, convreq.WithErrorHandler(errorHandler)))
	http.Handle("/rpcz", rpcz.Handler)
	http.Handle("/", convreq.Wrap(renderStatic))
	log.Printf("web server listening on addr %s.", addr)
	if err := http.ListenAndServe(addr, authMiddleWare(http.DefaultServeMux)); err != nil {
		log.Fatalf("failed to start HTTP server on address %q: %v", addr, err)
	}
}

func parseACLs(acls string) ([]*net.IPNet, error) {
	var ret []*net.IPNet
	for _, a := range strings.Split(acls, ",") {
		a = strings.TrimSpace(a)
		_, n, err := net.ParseCIDR(a)
		if err != nil {
			return nil, err
		}
		ret = append(ret, n)
	}
	return ret, nil
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
	for _, a := range acls {
		if a.Contains(ip) {
			return true
		}
	}
	return false
}

func authMiddleWare(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkRemoteAddr(r.RemoteAddr) {
			errorResponse(403, "Request from "+r.RemoteAddr+" denied (you can set --http_allow_hosts to allow remote hosts)").Respond(w, r)
			return
		}
		if password != "" {
			_, pw, ok := r.BasicAuth()
			if !ok || password != pw {
				respond.WithHeader(errorResponse(http.StatusUnauthorized, "Unauthorized"), "WWW-Authenticate", "Basic").Respond(w, r)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
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
		Ok      bool
		Version string
	}
	return respondJSON(Res{true, version.GetVersion()})
}

func renderHostname(ctx context.Context, req *http.Request) convreq.HttpResponse {
	type Res struct {
		Ok       bool
		Hostname string
	}
	h, err := os.Hostname()
	return respondJSON(Res{err == nil, h})
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
		Ok     bool
		Shares []string
	}
	res := Res{Ok: true}
	// TODO(quis): Only readdir get.Circle.
	dir := vfs.Readdir(ctx, "")
	for fn, fi := range dir.Files {
		if fi.IsDir() {
			res.Shares = append(res.Shares, fn)
		}
	}
	sort.Strings(res.Shares)
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
	go browser.OpenURL(mp)
	return respondJSON(map[string]bool{"ok": true})
}

func renderStatic(ctx context.Context, req *http.Request) convreq.HttpResponse {
	path := req.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	fh, err := staticFiles.Open("dist" + path)
	if err != nil {
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
	return respond.WithHeader(respond.ServeContent(path, time.Time{}, fh.(io.ReadSeeker)), "Content-Type", t)
}
