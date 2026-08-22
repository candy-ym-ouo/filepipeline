package storage

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"filepipeline/internal/domain"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Local struct {
	dir string
}

func NewLocal(dir string) (*Local, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("storage directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	return &Local{dir: dir}, nil
}
func (s *Local) Save(ctx context.Context, original string, src io.Reader, limit int64) (StoredFile, error) {
	if limit <= 0 {
		limit = domain.MaxFileSize
	}
	ext := strings.ToLower(filepath.Ext(filepath.Base(original)))
	name := "f_" + randomHex(12) + safeExtension(ext)
	path := s.Path(name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return StoredFile{}, fmt.Errorf("create stored file: %w", err)
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	prefix := make([]byte, 0, 512)
	reader := bufio.NewReader(src)
	buf := make([]byte, 32*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return StoredFile{}, err
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > limit {
				return StoredFile{}, domain.NewError(domain.ErrFileTooLarge, "文件超过 10MiB 限制")
			}
			need := 512 - len(prefix)
			if need > n {
				need = n
			}
			if need > 0 {
				prefix = append(prefix, buf[:need]...)
			}
			if _, err := file.Write(buf[:n]); err != nil {
				return StoredFile{}, fmt.Errorf("write stored file: %w", err)
			}
			_, _ = hash.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return StoredFile{}, fmt.Errorf("read upload: %w", readErr)
		}
	}
	if err := file.Sync(); err != nil {
		return StoredFile{}, fmt.Errorf("sync stored file: %w", err)
	}
	ok = true
	mime := "application/octet-stream"
	if len(prefix) > 0 {
		mime = http.DetectContentType(prefix)
	}
	return StoredFile{Name: name, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), MIME: mime}, nil
}
func (s *Local) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	file, err := os.Open(s.Path(name))
	if err != nil {
		return nil, fmt.Errorf("open stored file: %w", err)
	}
	return file, nil
}
func (s *Local) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	if err := os.Remove(s.Path(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (s *Local) Path(name string) string { return filepath.Join(s.dir, filepath.Base(name)) }
func (s *Local) Checksum(ctx context.Context, name string) (string, error) {
	file, err := s.Open(ctx, name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buf)
		if n > 0 {
			_, _ = hash.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func validateName(name string) error {
	if name == "" || filepath.Base(name) != name || strings.Contains(name, "..") {
		return fmt.Errorf("invalid stored filename")
	}
	return nil
}
func safeExtension(ext string) string {
	if len(ext) > 12 || strings.ContainsAny(ext, `/\\\x00`) {
		return ""
	}
	return ext
}
func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", os.Getpid())
	}
	return hex.EncodeToString(buf)
}
