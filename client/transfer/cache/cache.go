package cache

import (
	"io/ioutil"
	"os"
)

func New(size int64) (*Cache, error) {
	f, err := ioutil.TempFile("", "rufs-cache")
	if err != nil {
		return nil, err
	}
	fn := f.Name()
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
	os.Remove(c.Name())
	return c.File.Close()
}
