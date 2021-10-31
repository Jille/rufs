package local_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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
			if tries >= 10 {
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

func genFile(t *testing.T, name string, size int) {
	t.Helper()
	b := make([]byte, size)
	for i := 0; size > i; i++ {
		b[i] = byte(i % 256)
	}
	if err := os.WriteFile(name, b, 0644); err != nil {
		t.Fatalf("Failed to create random file %q: %v", name, err)
	}
}

func verifyReads(t *testing.T, path string, offset, size int) {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Errorf("Failed to open %q: %v", path, err)
		return
	}
	defer fh.Close()
	buf := make([]byte, size)
	n, err := fh.ReadAt(buf, int64(offset))
	if err != nil {
		t.Errorf("Failed to read %q at %d (%d bytes): %v", path, offset, size, err)
		return
	}
	if n != size {
		t.Errorf("Short read on %q: Got %d/%d", path, n, size)
	}
	wrong := 0
	for i := 0; n > i; i++ {
		if buf[i] != byte((offset+i)%256) {
			wrong++
		}
	}
	if wrong > 0 {
		t.Errorf("Incorrect data when reading from %q: %d/%d bytes are incorrect", path, wrong, size)
	}
}

func TestBasic(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "rufs-test-")
	if err != nil {
		t.Fatalf("Couldn't create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(baseDir) })

	circleName := "localhost:11000"

	must(t, os.Mkdir(filepath.Join(baseDir, "client-1"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "cfg"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "mnt"), 0755))
	must(t, os.Mkdir(filepath.Join(baseDir, "client-1", "shareA"), 0755))
	genFile(t, filepath.Join(baseDir, "client-1", "shareA", "file-0.dat"), 0)
	genFile(t, filepath.Join(baseDir, "client-1", "shareA", "file-1024.dat"), 1024)
	genFile(t, filepath.Join(baseDir, "client-1", "shareA", "file-8191.dat"), 8191)
	genFile(t, filepath.Join(baseDir, "client-1", "shareA", "file-8192.dat"), 8192)
	genFile(t, filepath.Join(baseDir, "client-1", "shareA", "file-10M.dat"), 10*1024*1024)
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

	mustRunCommand(t, getRUFSBinary("create_ca_pair"), "--circle="+circleName, "--certdir="+filepath.Join(baseDir, "discovery"))
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

	mustApiRequest(t, 11011, "register", map[string]string{"user": "client-1", "token": client1Token, "circle": circleName, "ca": caFingerprint})
	mustApiRequest(t, 11011, "shares_in_circle", map[string]string{"circle": circleName})
	mustApiRequest(t, 11011, "add_share", map[string]string{"circle": circleName, "share": "shareA", "local": filepath.Join(baseDir, "client-1", "shareA")})
	mustApiRequest(t, 11011, "add_share", map[string]string{"circle": circleName, "share": "shareB", "local": filepath.Join(baseDir, "client-1", "shareB")})

	waitForPort(t, 11021)

	mustApiRequest(t, 11021, "register", map[string]string{"user": "client-2", "token": client2Token, "circle": circleName, "ca": caFingerprint})
	resp = mustApiRequest(t, 11021, "shares_in_circle", map[string]string{"circle": circleName})
	if diff := cmp.Diff(resp.Shares, []string{"shareA", "shareB"}); diff != "" {
		t.Errorf("shares_in_circle returned incorrect list: %s", diff)
	}
	mustApiRequest(t, 11021, "add_share", map[string]string{"circle": circleName, "share": "shareA", "local": filepath.Join(baseDir, "client-2", "shareA")})

	mustRunCommand(t, "md5sum", filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-0.dat"))
	verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-1024.dat"), 0, 1024)
	verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-1024.dat"), 0, 512)
	verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-1024.dat"), 0, 511)
	verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-8191.dat"), 0, 8191)
	verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-8192.dat"), 0, 8192)
	verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-8192.dat"), 4095, 4097)
	for i := 0; 1000 > i; i++ {
		off := rand.Intn(10 * 1024 * 1024)
		siz := rand.Intn(1024 * 1024)
		if off+siz > 10*1024*1024 {
			siz = 10*1024*1024 - off
		}
		verifyReads(t, filepath.Join(baseDir, "client-2", "mnt", "all", "shareA", "file-10M.dat"), off, siz)
	}
}
