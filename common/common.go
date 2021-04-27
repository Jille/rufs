package common

import "strings"

func CircleFromPeer(peer string) string {
	return strings.Split(peer, "@")[1]
}

func SplitMaybeEmpty(str, sep string) []string {
	if str == "" {
		return nil
	}
	return strings.Split(str, sep)
}
