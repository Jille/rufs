package common

import "strings"

func CircleFromPeer(peer string) string {
	return strings.Split(peer, "@")[1]
}
