package readonlyhandle

import (
	"errors"
	"io"
	"os"

	"github.com/go-git/go-billy/v5"
)

func New(r io.ReaderAt, name string) billy.File {
	return &billyFilePolyFill{
		r:    r,
		name: name,
	}
}

type billyFilePolyFill struct {
	r          io.ReaderAt
	name       string
	readOffset int64
}

func (p *billyFilePolyFill) Name() string {
	return p.name
}

func (p *billyFilePolyFill) Write(buf []byte) (int, error) {
	return 0, os.ErrPermission
}

func (p *billyFilePolyFill) WriteAt(buf []byte, offset int64) (int, error) {
	return 0, os.ErrPermission
}

func (p *billyFilePolyFill) Read(buf []byte) (int, error) {
	n, err := p.r.ReadAt(buf, p.readOffset)
	p.readOffset += int64(n)
	return n, err
}

func (p *billyFilePolyFill) ReadAt(buf []byte, offset int64) (int, error) {
	return p.r.ReadAt(buf, offset)
}

func (p *billyFilePolyFill) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		p.readOffset = offset
	case io.SeekCurrent:
		p.readOffset += offset
	case io.SeekEnd:
		return 0, errors.New("not implemented")
	}
	return p.readOffset, nil
}

func (p *billyFilePolyFill) Close() error {
	if c, ok := p.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (p *billyFilePolyFill) Lock() error {
	return os.ErrPermission
}

func (p *billyFilePolyFill) Unlock() error {
	return os.ErrPermission
}

func (p *billyFilePolyFill) Truncate(size int64) error {
	return os.ErrPermission
}
