package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pkg/browser"
	"github.com/sgielen/rufs/client/config"
	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/content"
	"github.com/sgielen/rufs/client/fuse"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/shares"
	"github.com/sgielen/rufs/client/systray"
	"github.com/sgielen/rufs/client/web"
	"github.com/sgielen/rufs/common"
	"github.com/sgielen/rufs/security"
)

var (
	discoveryPort = flag.Int("discovery-port", 12000, "Port of the discovery server")
	flag_endp     = flag.String("endpoints", "", "Override our RuFS endpoints (comma-separated IPs or IP:port, autodetected if empty)")
	port          = flag.Int("port", 12010, "content server listen port")
	httpPort      = flag.Int("http_port", -1, "HTTP server listen port (default: port+1; default 12011)")
	allowUsers    = flag.String("allow_users", "", "Which local users to allow access to the fuse mount, comma separated")
	mountpoint    = flag.String("mountpoint", "", "Where to mount everyone's stuff")
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile | log.Lmicroseconds)
	flag.Parse()
	ctx := context.Background()

	if *mountpoint == "" {
		log.Fatalf("--mountpoint must not be empty (see -help)")
	}

	if err := config.LoadConfig(); err != nil {
		log.Printf("failed to load configuration: %v", err)
		config.LoadEmptyConfig()
	}

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
		browser.OpenURL(address)
		log.Printf("no circles configured - visit %s to start rufs configuration.", address)
	}

	connectToCircles(circles)

	go func() {
		f, err := fuse.NewMount(*mountpoint, *allowUsers)
		if err != nil {
			log.Fatalf("failed to mount fuse: %v", err)
		}
		if err := f.Run(ctx); err != nil {
			log.Fatalf("failed to run fuse: %v", err)
		}
		os.Exit(0)
	}()

	systray.Run(onOpen, onSettings, func() {})
}

func onOpen() {
	browser.OpenURL(*mountpoint)
}

func onSettings() {
	address := fmt.Sprintf("http://127.0.0.1:%d/", *httpPort)
	browser.OpenURL(address)
}

func connectToCircles(circles map[string]*security.KeyPair) {
	ctx := context.Background()
	for circle, kp := range circles {
		// connectivity.ConnectToCircle returns nil if we already connected to a circle.
		if err := connectivity.ConnectToCircle(ctx, circle, *discoveryPort, common.SplitMaybeEmpty(*flag_endp, ","), *port, kp); err != nil {
			log.Fatalf("Failed to connect to circle %q: %v", circle, err)
		}
		metrics.SetClientStartTimeSeconds([]string{circle}, time.Now())
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
