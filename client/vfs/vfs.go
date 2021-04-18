package vfs

type VFS struct {
}

type Directory struct {
	SubDirs []string
	Files []File
}

type File struct {
	FullPath string
	Mtime time.Time
}

func (f File) Basename() string {
	return filepath.Base(f.FullPath)
}

func (fs *VFS) Readdir(path string) (Directory, error) {
	// TODO
	return nil, nil
}
