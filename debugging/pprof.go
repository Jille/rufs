package debugging

import (
	"net/http"
	"runtime/pprof"

	// Imported for /debug/pprof/...
	_ "net/http/pprof"
)

func init() {
	http.Handle("/debug/pprof/threads", http.HandlerFunc(serveStackTraces))
}

func serveStackTraces(w http.ResponseWriter, req *http.Request) {
	pprof.Lookup("goroutine").WriteTo(w, 1)
}
