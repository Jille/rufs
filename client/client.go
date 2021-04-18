package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/content"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
)

var (
	circle        = flag.String("circle", "", "Name of the circle to join")
	discoveryPort = flag.Int("discovery-port", 12000, "Port of the discovery server")
	username      = flag.String("user", "", "RuFS username")
	flag_endp     = flag.String("endpoints", "", "Override our RuFS endpoints (comma-separated IPs or IP:port, autodetected if empty)")
	port          = flag.Int("port", 12010, "content server listen port")
	path          = flag.String("path", "", "root to served content")
)

func splitMaybeEmpty(str, sep string) []string {
	if str == "" {
		return nil
	}
	return strings.Split(str, sep)
}

func main() {
	flag.Parse()
	ctx := context.Background()

	if *circle == "" || *username == "" {
		log.Fatalf("--circle and --username must not be empty (see -help)")
	}

	tc, err := security.TLSConfigForMasterClient("/tmp/rufs/ca.crt", fmt.Sprintf("/tmp/rufs/%s@%s.crt", *username, *circle), fmt.Sprintf("/tmp/rufs/%s@%s.key", *username, *circle))
	if err != nil {
		log.Fatalf("Failed to load certificates: %v", err)
	}

	if err := connectivity.ConnectToCircle(ctx, net.JoinHostPort(*circle, fmt.Sprint(*discoveryPort)), splitMaybeEmpty(*flag_endp, ","), *port, tc); err != nil {
		log.Fatalf("Failed to connect to circle %q: %v", *circle, err)
	}

	content, err := content.New(fmt.Sprintf(":%d", *port), *path)
	if err != nil {
		log.Fatalf("failed to create content server: %v", err)
	}
	go content.Run()

	for {
		readdir(ctx, "/")
		time.Sleep(10 * time.Second)
	}
}

func readdir(ctx context.Context, path string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	type peerFileInstance struct {
		peer        *connectivity.Peer
		isDirectory bool
	}
	type peerFile struct {
		instances []*peerFileInstance
	}

	warnings := []string{}
	files := make(map[string]*peerFile)

	for _, peer := range connectivity.AllPeers() {
		r, err := peer.ContentServiceClient().ReadDir(ctx, &pb.ReadDirRequest{
			Path: path,
		})
		if err != nil {
			log.Printf("failed to readdir on peer %s, ignoring: %v", peer.Name, err)
			continue
		}
		for _, file := range r.Files {
			if files[file.Filename] == nil {
				files[file.Filename] = &peerFile{}
			}
			instance := &peerFileInstance{
				peer:        peer,
				isDirectory: file.GetIsDirectory(),
			}
			files[file.Filename].instances = append(files[file.Filename].instances, instance)
		}
	}

	// remove files available on multiple peers, unless they are a directory everywhere
	for filename, file := range files {
		if len(file.instances) != 1 {
			peers := ""
			isDirectoryEverywhere := true
			for _, instance := range file.instances {
				if !instance.isDirectory {
					isDirectoryEverywhere = false
				}
				if peers == "" {
					peers = instance.peer.Name
				} else {
					peers = peers + ", " + instance.peer.Name
				}
			}
			if !isDirectoryEverywhere {
				warnings = append(warnings, fmt.Sprintf("File %s is available on multiple peers (%s), so it was hidden.", filename, peers))
				delete(files, filename)
			}
		}
	}

	log.Printf("files in %s:", path)
	for filename, file := range files {
		log.Printf("- %s (%s)", filename, *file)
	}
	if len(warnings) >= 1 {
		log.Printf("warnings:")
		for _, warning := range warnings {
			log.Printf("- %s", warning)
		}
	}
}
