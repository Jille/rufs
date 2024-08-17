package shares

import (
	"github.com/Jille/rufs/client/config"
	"github.com/Jille/rufs/common"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/ory/go-convenience/stringslice"
)

func SharesForPeer(name string) map[string]billy.Filesystem {
	cn := common.CircleFromPeer(name)
	c, ok := config.GetCircle(cn)
	if !ok {
		return nil
	}
	ret := map[string]billy.Filesystem{}
	for _, s := range c.Shares {
		if stringslice.Has(s.Writers, name) {
			ret[s.Remote] = osfs.New(s.Local)
		}
	}
	return ret
}

func HasAnyDirectIOShares() bool {
	for _, c := range config.GetCircles() {
		for _, s := range c.Shares {
			if len(s.Writers) > 0 {
				return true
			}
		}
	}
	return false
}
