package config

import "flag"

var (
	// Both variables are protected by mtx.
	previousMountpoint string
	mountpointListener = func(string) {}

	mountpointFlag = flag.String("mountpoint", "", "Where to mount everyone's stuff")
)

func SetMountpoint(m string) {
	if *mountpointFlag != "" {
		return
	}
	mtx.Lock()
	if m == previousMountpoint {
		mtx.Unlock()
		return
	}
	previousMountpoint = m
	cb := mountpointListener
	mtx.Unlock()
	cb(m)
}

func RegisterMountpointListener(cb func(string)) {
	mtx.Lock()
	mountpointListener = cb
	p := previousMountpoint
	mtx.Unlock()
	if *mountpointFlag != "" {
		p = *mountpointFlag
	}
	if p != "" {
		cb(p)
	}
}

func GetMountpoint() string {
	if *mountpointFlag != "" {
		return *mountpointFlag
	}
	mtx.Lock()
	defer mtx.Unlock()
	return previousMountpoint
}
