package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"filepipeline/internal/domain"
	"filepipeline/internal/storage"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Extractor struct {
	storage storage.Storage
}

func NewExtractor(store storage.Storage) *Extractor { return &Extractor{storage: store} }
func (e *Extractor) Extract(ctx context.Context, task domain.Task) (string, string, error) {
	file, err := e.storage.Open(ctx, task.StoredName)
	if err != nil {
		return "", "", domain.WrapError(domain.ErrExtract, "打开文件失败", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, domain.MaxFileSize+1))
	if err != nil {
		return "", "", domain.WrapError(domain.ErrExtract, "读取文件失败", err)
	}
	if int64(len(data)) > domain.MaxFileSize {
		return "", "", domain.NewError(domain.ErrExtract, "提取时文件超过大小限制")
	}
	ext := strings.ToLower(filepath.Ext(task.Filename))
	var summary string
	switch ext {
	case ".txt", ".log", ".md":
		summary = extractText(data)
	case ".json":
		summary, err = extractJSON(data)
	case ".csv":
		summary, err = extractCSV(data)
	case ".yaml", ".yml":
		summary, err = extractYAML(data)
	case ".xml":
		summary, err = extractXML(data)
	default:
		err = fmt.Errorf("unsupported extension %s", ext)
	}
	if err != nil {
		return "", "", domain.WrapError(domain.ErrExtract, "内容提取失败", err)
	}
	summary = truncateSummary(summary)
	return summary, "内容提取完成", nil
}
func extractText(data []byte) string {
	text := string(data)
	lines := strings.Split(text, "\n")
	words := 0
	for _, field := range strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }) {
		if field != "" {
			words++
		}
	}
	previewLines := lines
	if len(previewLines) > 20 {
		previewLines = previewLines[:20]
	}
	return fmt.Sprintf("type: text\nlines: %d\ncharacters: %d\nwords: %d\nempty: %t\npreview:\n%s",
		len(lines), utf8.RuneCountInString(text), words, len(strings.TrimSpace(text)) == 0,
		strings.Join(previewLines, "\n"))
}
func extractJSON(data []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("multiple JSON values")
		}
		return "", err
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return fmt.Sprintf("type: object\nkeys: %s\nlength: %d\nempty: %t", strings.Join(keys, ", "), len(typed), len(typed) == 0), nil
	case []any:
		return fmt.Sprintf("type: array\nlength: %d\nempty: %t", len(typed), len(typed) == 0), nil
	default:
		return fmt.Sprintf("type: %T\nvalue: %v", typed, typed), nil
	}
}
func extractCSV(data []byte) (string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "type: csv\nempty: true", nil
	}
	header := records[0]
	samples := records[1:]
	if len(samples) > 3 {
		samples = samples[:3]
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "type: csv\ncolumns: %s\nrows: %d\n", strings.Join(header, ", "), max(0, len(records)-1))
	for index, row := range samples {
		fmt.Fprintf(&builder, "sample_%d: %s\n", index+1, strings.Join(row, " | "))
	}
	return strings.TrimSpace(builder.String()), nil
}
func extractYAML(data []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	keys := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if len(line) != len(strings.TrimLeft(line, " \t")) {
			continue
		}
		if index := strings.Index(line, ":"); index > 0 {
			keys = append(keys, strings.TrimSpace(line[:index]))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("type: yaml\ntop_level_keys: %s\nempty: %t", strings.Join(unique(keys), ", "), len(keys) == 0), nil
}
func extractXML(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, root := 0, ""
	children := make([]string, 0)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				root = value.Name.Local
			} else if depth == 1 {
				children = append(children, value.Name.Local)
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	if root == "" {
		return "", fmt.Errorf("missing root element")
	}
	return fmt.Sprintf("type: xml\nroot: %s\nchildren: %s", root, strings.Join(unique(children), ", ")), nil
}
func truncateSummary(value string) string {
	if len(value) <= domain.MaxSummarySize {
		return value
	}
	marker := "\n[truncated: true]"
	limit := domain.MaxSummarySize - len(marker)
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + marker
}
func unique(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
