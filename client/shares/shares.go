package shares

import (
	"errors"
	"os"

	pb "github.com/sgielen/rufs/proto"
)

func Readdir(circle, remotePath string) ([]*pb.File, error) {
	return nil, errors.New("not yet implemented")
}

func Open(circle, remotePath string) (*os.File, error) {
	return nil, errors.New("not yet implemented")
}

func GetHash(circle, remotePath string, callback func(hash string)) {
}
