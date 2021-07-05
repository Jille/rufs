package udptransport

import (
	"os"
)

type deadlineExceeded struct {
}

func (deadlineExceeded) Error() string {
	return "udptransport io deadline exceeded"
}

func (deadlineExceeded) Unwrap() error {
	return os.ErrDeadlineExceeded
}
