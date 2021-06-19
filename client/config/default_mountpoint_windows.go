// +build windows

package config

func defaultMountpoint() (string, error) {
	return "R:", nil
}
