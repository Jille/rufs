package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/Jille/rufs/client/config"
	"github.com/Jille/rufs/client/connectivity"
	"github.com/Jille/rufs/client/content"
	"github.com/Jille/rufs/client/fuse"
	"github.com/Jille/rufs/client/metrics"
	"github.com/Jille/rufs/client/shares"
	"github.com/Jille/rufs/client/systray"
	"github.com/Jille/rufs/client/vfs"
	"github.com/Jille/rufs/client/web"
	"github.com/Jille/rufs/common"
	"github.com/Jille/rufs/security"
	"github.com/Jille/rufs/version"
	"github.com/pkg/browser"
)

var (
	flag_endp          = flag.String("endpoints", "", "Override our RuFS endpoints (comma-separated IPs or IP:port, autodetected if empty)")
	port               = flag.Int("port", 12010, "content server listen port")
	httpPort           = flag.Int("http_port", -1, "HTTP server listen port (default: port+1; default 12011)")
	allowUsers         = flag.String("allow_users", "", "Which local users to allow access to the fuse mount, comma separated")
	readdirCacheTarget = flag.Int("readdir_cache_target", 1000, "readdir cache size target. Affects memory usage. Set to 0 to disable cache")
	versionFlag        = flag.Bool("version", false, "Print the version and exit")

	unmountFuse = func() {}
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile | log.Lmicroseconds)
	flag.Parse()
	ctx := context.Background()

	if *versionFlag {
		log.Printf("RUFS client version %s", version.GetVersion())
		return
	}

	log.Printf("starting rufs client %s", version.GetVersion())

	if err := config.LoadConfig(); err != nil {
		log.Printf("failed to load configuration: %v", err)
		config.LoadEmptyConfig()
	}

	vfs.InitCache(*readdirCacheTarget)
	metrics.Init()
	shares.Init()

	circles, err := config.LoadAllCerts()
	if err != nil {
		log.Fatalf("Failed to read certificates: %v", err)
	}
	var kps []*security.KeyPair
	for _, kp := range circles {
		kps = append(kps, kp)
	}

	if err := content.Serve(fmt.Sprintf(":%d", *port), kps); err != nil {
		log.Fatalf("failed to create content server: %v", err)
	}

	if *httpPort == -1 {
		*httpPort = *port + 1
	}
	web.ReloadConfigCallback = ReloadConfig
	go web.Init(fmt.Sprintf(":%d", *httpPort))

	if len(circles) == 0 {
		address := fmt.Sprintf("http://127.0.0.1:%d/", *httpPort)
		go browser.OpenURL(address)
		log.Printf("no circles configured - visit %s to start rufs configuration.", address)
	}

	connectToCircles(circles)

	config.RegisterMountpointListener(func(mp string) {
		unmountFuse()
		ctx, cancel := context.WithCancel(ctx)
		unmountFuse = cancel
		go func() {
			isCreated, err := maybeCreateMountpoint(mp)
			if err != nil {
				log.Printf("failed to create mountpoint: %v", err)
			}
			f, err := fuse.NewMount(mp, *allowUsers)
			if err != nil {
				log.Fatalf("failed to mount fuse: %v", err)
			}
			if err := f.Run(ctx); err != nil {
				log.Fatalf("failed to run fuse: %v", err)
			}
			if isCreated {
				if err := os.Remove(mp); err != nil {
					log.Fatalf("failed to clean up mointpoint: %v", err)
				}
			}
			os.Exit(0)
		}()
	})

	systray.Run(onOpen, onSettings, func() {
		unmountFuse()
		// Wait a second to allow fuse to unmount
		time.Sleep(1 * time.Second)
	})

	// Code here will not run. Instead, put it in the onQuit lambda above.
}

func maybeCreateMountpoint(mp string) (bool, error) {
	// On Windows, never create the mountpoint
	if runtime.GOOS == "windows" {
		return false, nil
	}
	// On Unix, create the mountpoint if it does not exist yet
	_, err := os.Stat(mp)
	if os.IsNotExist(err) {
		return true, os.MkdirAll(mp, 0777)
	}
	return false, err
}

func onOpen() {
	mp := config.GetMountpoint()
	if mp == "" {
		onSettings()
		return
	}
	go browser.OpenURL(mp)
}

func onSettings() {
	address := fmt.Sprintf("http://127.0.0.1:%d/", *httpPort)
	go browser.OpenURL(address)
}

func connectToCircles(circles map[string]*security.KeyPair) {
	ctx := context.Background()
	for circle, kp := range circles {
		circle := circle
		kp := kp
		go func() {
			// connectivity.ConnectToCircle returns nil if we already connected to a circle.
			if err := connectivity.ConnectToCircle(ctx, circle, common.SplitMaybeEmpty(*flag_endp, ","), *port, kp); err != nil {
				log.Fatalf("Failed to connect to circle %q: %v", circle, err)
			}
			metrics.SetClientStartTimeSeconds([]string{circle}, time.Now())
			metrics.SetClientVersion([]string{circle}, version.GetVersion(), 1)
		}()
	}
}

func ReloadConfig() {
	metrics.ReloadConfig()
	if err := shares.ReloadConfig(); err != nil {
		log.Fatalf("Failed to reload shares: %v", err)
	}
	circles, err := config.LoadAllCerts()
	if err != nil {
		log.Fatalf("Failed to read certificates: %v", err)
	}
	var kps []*security.KeyPair
	for _, kp := range circles {
		kps = append(kps, kp)
	}
	content.SwapKeyPairs(kps)
	connectToCircles(circles)
}
