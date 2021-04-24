// Package passive opens PassiveTransfer streams to all given peers, handles uploads and downloads and reports back updates.
package passive

import (
	"context"
	"io"
)

type backend interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}

type TransferClient interface {
	ReceivedBytes(start, end int64)
	SetConnectedPeers(peers []string)
	UploadFailed(peer string)
}

func New(ctx context.Context, storage backend, callbacks TransferClient) *Transfer {
}

type Transfer struct {
}

func (t *Transfer) SetPeers(peers []string) {
}

func (t *Transfer) Upload(ctx context.Context, peer string, byteRange *pb.Range) {
}

func (t *Transfer) Close() error {
}
