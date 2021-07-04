package local_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func mustRunCommand(t *testing.T, argv ...string) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command %s failed: %v", strings.Join(argv, " "), err)
	}
}

func mustRunCommand_Background(t *testing.T, argv ...string) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Command %s failed to start: %v", strings.Join(argv, " "), err)
	}
	dying := make(chan struct{})
	go func() {
		if err := cmd.Wait(); err != nil {
			select {
			case <-dying:
				return
			default:
			}
			t.Errorf("Command %s failed: %v", strings.Join(argv, " "), err)
		}
	}()
	t.Cleanup(func() {
		close(dying)
		if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
			t.Errorf("Failed to kill %s: %v", argv[0], err)
		}
	})
}

func mustRunCommand_GetStdout(t *testing.T, argv ...string) string {
	var buf bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command %s failed: %v", strings.Join(argv, " "), err)
	}
	return buf.String()
}

func getRUFSBinary(name string) string {
	return filepath.Join("bin", name)
}

// Only use this for trivial setup that doesn't need its error explained.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
}

func waitForPort(t *testing.T, port int) {
	tr := time.NewTicker(100 * time.Millisecond)
	defer tr.Stop()
	tries := 0
	for range tr.C {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			tries++
			if tries >= 5 {
				t.Fatalf("Port %d didn't come up: %v", port, err)
			}
			continue
		}
		c.Close()
		return
	}
}

type jsonResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`

	Shares  []string `json:"shares"`
	Version string   `json:"version"`
}

func apiRequest(t *testing.T, port int, api string, args map[string]string) jsonResponse {
	t.Helper()
	uv := url.Values{}
	for k, v := range args {
		uv.Set(k, v)
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/%s?%s", port, api, uv.Encode()), nil)
	if err != nil {
		t.Fatalf("Failed to construct HTTP request: %v", err)
	}
	log.Printf("Sending API request: /api/%s: %v", api, args)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send HTTP request: %v", err)
	}
	var ret jsonResponse
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		t.Fatalf("Failed to parse response JSON from /api/%s: %v", api, err)
	}
	return ret
}

func mustApiRequest(t *testing.T, port int, api string, args map[string]string) jsonResponse {
	ret := apiRequest(t, port, api, args)
	if ret.Error != "" {
		t.Fatalf("Request to /api/%s failed: %v", api, ret.Error)
	}
	if !ret.OK {
		t.Fatalf("Request to /api/%s failed without error message", api)
	}
	return ret
}

func TestBasic(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "rufs-test-")
	if err != nil {
		t.Fatalf("Couldn't create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(baseDir) })

	must(t, os.Mkdir(filepath.Join(baseDir, "client-1"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "cfg"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "mnt"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "shareA"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "shareB"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-2"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-2", "cfg"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-2", "mnt"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-2", "shareA"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "discovery"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "discovery", "logs"), 0755))

	// TODO(#27): Don't start a browser.
	must(t, os.Mkdir(filepath.Join(baseDir, "path"), 0755))
	mustRunCommand(t, "touch", filepath.Join(baseDir, "path", "xdg-open"))
	mustRunCommand(t, "chmod", "755", filepath.Join(baseDir, "path", "xdg-open"))
	os.Setenv("PATH", fmt.Sprintf("%s:%s", filepath.Join(baseDir, "path"), os.Getenv("PATH")))

	mustRunCommand(t, getRUFSBinary("create_ca_pair"), "--circle=localhost.quis.cx:11000", "--certdir="+filepath.Join(baseDir, "discovery"))
	caFingerprint := strings.TrimSpace(mustRunCommand_GetStdout(t, getRUFSBinary("ca_fingerprint"), "--certdir="+filepath.Join(baseDir, "discovery")))
	client1Token := strings.TrimSpace(mustRunCommand_GetStdout(t, getRUFSBinary("create_auth_token"), "--certdir="+filepath.Join(baseDir, "discovery"), "client-1"))
	client2Token := strings.TrimSpace(mustRunCommand_GetStdout(t, getRUFSBinary("create_auth_token"), "--certdir="+filepath.Join(baseDir, "discovery"), "client-2"))

	mustRunCommand_Background(t, getRUFSBinary("discovery"), "--port=11000", "--certdir="+filepath.Join(baseDir, "discovery"), "--collected_logs_file="+filepath.Join(baseDir, "discovery", "logs"))
	mustRunCommand_Background(t, getRUFSBinary("client"), "--port=11010", "--config="+filepath.Join(baseDir, "client-1", "cfg"), "--mountpoint="+filepath.Join(baseDir, "client-1", "mnt"))
	t.Cleanup(func() {
		exec.Command("fusermount", "-u", filepath.Join(baseDir, "client-1", "mnt")).Run()
	})
	mustRunCommand_Background(t, getRUFSBinary("client"), "--port=11020", "--config="+filepath.Join(baseDir, "client-2", "cfg"), "--mountpoint="+filepath.Join(baseDir, "client-2", "mnt"))
	t.Cleanup(func() {
		exec.Command("fusermount", "-u", filepath.Join(baseDir, "client-2", "mnt")).Run()
	})

	waitForPort(t, 11011)

	resp := mustApiRequest(t, 11011, "version", nil)
	log.Printf("client-1 is running %q", resp.Version)

	waitForPort(t, 11000)

	mustApiRequest(t, 11011, "register", map[string]string{"user": "client-1", "token": client1Token, "circle": "localhost.quis.cx:11000", "ca": caFingerprint})
	mustApiRequest(t, 11011, "shares_in_circle", map[string]string{"circle": "localhost.quis.cx:11000"})
	mustApiRequest(t, 11011, "add_share", map[string]string{"circle": "localhost.quis.cx:11000", "share": "shareA", "local": filepath.Join(baseDir, "client-1", "shareA")})
	mustApiRequest(t, 11011, "add_share", map[string]string{"circle": "localhost.quis.cx:11000", "share": "shareB", "local": filepath.Join(baseDir, "client-1", "shareB")})

	waitForPort(t, 11021)

	mustApiRequest(t, 11021, "register", map[string]string{"user": "client-2", "token": client2Token, "circle": "localhost.quis.cx:11000", "ca": caFingerprint})
	resp = mustApiRequest(t, 11021, "shares_in_circle", map[string]string{"circle": "localhost.quis.cx:11000"})
	if diff := cmp.Diff(resp.Shares, []string{"shareA", "shareB"}); diff != "" {
		t.Errorf("shares_in_circle returned incorrect list: %s", diff)
	}
	mustApiRequest(t, 11021, "add_share", map[string]string{"circle": "localhost.quis.cx:11000", "share": "shareA", "local": filepath.Join(baseDir, "client-2", "shareA")})
}
