// +build !windows

package config

import "path/filepath"

func defaultMountpoint() (string, error) {
	d, err := homedir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "rufs"), nil
}
