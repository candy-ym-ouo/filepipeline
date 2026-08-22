package storage

import (
	"context"
	"io"
)

type StoredFile struct {
	Name   string
	Size   int64
	SHA256 string
	MIME   string
}
type Storage interface {
	Save(context.Context, string, io.Reader, int64) (StoredFile, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Remove(context.Context, string) error
	Path(string) string
	Checksum(context.Context, string) (string, error)
}
