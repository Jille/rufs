package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/getlantern/systray"
	"github.com/pkg/browser"
	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/content"
	"github.com/sgielen/rufs/client/fuse"
	"github.com/sgielen/rufs/client/icon"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/shares"
	"github.com/sgielen/rufs/client/web"
	"github.com/sgielen/rufs/common"
	"github.com/sgielen/rufs/config"
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

	circles := map[string]*security.KeyPair{}
	var kps []*security.KeyPair
	for _, c := range config.GetCircles() {
		kp, err := config.LoadCerts(c.Name)
		if err != nil {
			log.Fatalf("Failed to read certificates: %v", err)
		}
		circles[c.Name] = kp
		kps = append(kps, kp)
	}

	if err := content.Serve(fmt.Sprintf(":%d", *port), kps); err != nil {
		log.Fatalf("failed to create content server: %v", err)
	}

	if *httpPort == -1 {
		*httpPort = *port + 1
	}
	go web.Init(fmt.Sprintf(":%d", *httpPort))

	if len(circles) == 0 {
		address := fmt.Sprintf("http://127.0.0.1:%d/", *httpPort)
		browser.OpenURL(address)
		log.Printf("no circles configured - visit %s to start rufs configuration.", address)
	}

	for circle, kp := range circles {
		if err := connectivity.ConnectToCircle(ctx, circle, *discoveryPort, common.SplitMaybeEmpty(*flag_endp, ","), *port, kp); err != nil {
			log.Fatalf("Failed to connect to circle %q: %v", circle, err)
		}
		metrics.SetClientStartTimeSeconds([]string{circle}, time.Now())
	}

	go func() {
		f, err := fuse.NewMount(*mountpoint, *allowUsers)
		if err != nil {
			log.Fatalf("failed to mount fuse: %v", err)
		}
		if err := f.Run(ctx); err != nil {
			log.Fatalf("failed to run fuse: %v", err)
		}
	}()

	systray.Run(onSystrayReady, func() {})
}

func onSystrayReady() {
	systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("RUFS")
	systray.SetTooltip("RUFS")

	mOpen := systray.AddMenuItem("Open", "")
	mSettings := systray.AddMenuItem("Settings", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")
	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				browser.OpenURL("file://" + *mountpoint)
			case <-mSettings.ClickedCh:
				address := fmt.Sprintf("http://127.0.0.1:%d/", *httpPort)
				browser.OpenURL(address)
			case <-mQuit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}
