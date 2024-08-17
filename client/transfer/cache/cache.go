package cache

import (
	"io/ioutil"
	"os"
	"runtime"
)

func New(size int64) (*Cache, error) {
	f, err := ioutil.TempFile("", "rufs-cache")
	if err != nil {
		return nil, err
	}
	fn := f.Name()
	if runtime.GOOS != "windows" {
		// Unlink the file so it isn't left behind if we crash
		if err := os.Remove(fn); err != nil {
			return nil, err
		}
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		os.Remove(fn)
		return nil, err
	}
	return &Cache{f}, nil
}

type Cache struct {
	*os.File
}

func (c *Cache) Close() error {
	if runtime.GOOS == "windows" {
		os.Remove(c.Name())
	}
	return c.File.Close()
}
