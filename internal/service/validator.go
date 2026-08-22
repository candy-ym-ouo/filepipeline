package service

import (
	"bytes"
	"context"
	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Validator struct {
	storage storage.Storage
}

func NewValidator(store storage.Storage) *Validator { return &Validator{storage: store} }

var allowedExtensions = map[string]bool{
	".txt": true, ".json": true, ".csv": true, ".log": true,
	".md": true, ".yaml": true, ".xml": true,
}

func (v *Validator) Validate(ctx context.Context, task domain.Task) (string, error) {
	ext := strings.ToLower(filepath.Ext(task.Filename))
	if !allowedExtensions[ext] {
		return "", domain.NewError(domain.ErrExtension, "不支持的文件扩展名: "+ext)
	}
	if !mimeCompatible(ext, task.MIME) {
		return "", domain.NewError(domain.ErrMIME, fmt.Sprintf("扩展名 %s 与 MIME %s 不匹配", ext, task.MIME))
	}
	file, err := v.storage.Open(ctx, task.StoredName)
	if err != nil {
		return "", domain.WrapError(domain.ErrMagicMismatch, "无法读取文件", err)
	}
	defer file.Close()
	prefix, err := io.ReadAll(io.LimitReader(file, 64*1024))
	if err != nil {
		return "", domain.WrapError(domain.ErrMagicMismatch, "读取文件头失败", err)
	}
	if err := validateMagic(ext, prefix); err != nil {
		return "", err
	}
	if task.Size == 0 {
		return "", domain.NewError(domain.ErrEmptyFile, "空文件不允许处理")
	}
	if task.Size < 0 || task.Size > domain.MaxFileSize {
		return "", domain.NewError(domain.ErrFileTooLarge, "文件超过 10MiB 限制")
	}
	checksum, err := v.storage.Checksum(ctx, task.StoredName)
	if err != nil {
		return "", domain.WrapError(domain.ErrHashMismatch, "重算文件哈希失败", err)
	}
	if !strings.EqualFold(checksum, task.SHA256) {
		return "", domain.NewError(domain.ErrHashMismatch, "文件 SHA-256 与上传记录不一致")
	}
	return fmt.Sprintf("格式校验通过（%s，%d bytes）", ext, task.Size), nil
}
func mimeCompatible(ext, mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	if mime == "" {
		return false
	}
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	switch ext {
	case ".json":
		return mime == "application/json" || mime == "application/octet-stream"
	case ".xml":
		return mime == "application/xml" || mime == "application/octet-stream"
	case ".yaml", ".yml":
		return mime == "application/yaml" || mime == "application/x-yaml" || mime == "application/octet-stream"
	default:
		return mime == "application/octet-stream"
	}
}
func validateMagic(ext string, data []byte) error {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return domain.NewError(domain.ErrMagicMismatch, "文件包含二进制或非 UTF-8 内容")
	}
	trimmed := bytes.TrimSpace(data)
	if ext == ".json" && len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		return domain.NewError(domain.ErrMagicMismatch, "JSON 文件必须以对象或数组开头")
	}
	if ext == ".xml" && len(trimmed) > 0 && trimmed[0] != '<' {
		return domain.NewError(domain.ErrMagicMismatch, "XML 文件必须以标签开头")
	}
	if ext == ".csv" && len(data) > 0 {
		first := data
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			first = data[:index]
		}
		if len(bytes.TrimSpace(first)) == 0 {
			return domain.NewError(domain.ErrMagicMismatch, "CSV 首行不能为空")
		}
	}
	return nil
}
