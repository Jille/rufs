package common

import "strings"

func CircleFromPeer(peer string) string {
	return strings.Split(peer, "@")[1]
}

func CirclesFromPeers(peers []string) []string {
	cs := map[string]bool{}
	for _, p := range peers {
		cs[CircleFromPeer(p)] = true
	}
	ret := make([]string, 0, len(cs))
	for c := range cs {
		ret = append(ret, c)
	}
	return ret
}

func SplitMaybeEmpty(str, sep string) []string {
	if str == "" {
		return nil
	}
	return strings.Split(str, sep)
}
