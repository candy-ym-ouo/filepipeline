# filepipeline · 标准打包说明

这是一个基于 Go 和 SQLite 的文件处理流水线服务，提供文件上传、格式校验、内容提取、扫描、异步回调和失败重试能力。项目同时包含原生 HTML/CSS/JavaScript 前端。

## 本地验证

```bash
GOSUMDB=off GOPROXY=off make verify
```

等价检查包括：

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

## 本地运行

启动 API：

```bash
go run ./cmd/api
```

启动 Worker（本地使用内置 mock 扫描器）：

```bash
PIPE_SCAN_URL=mock://clean go run ./cmd/worker
```

然后访问 <http://127.0.0.1:8080>。

## Docker 打包

构建脚本会使用规范要求的 `benzhi.Dockerfile`，并保留完整 Go 工具链：

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh filepipeline linux/amd64
./build_benzhi_docker.sh filepipeline linux/arm64
```

进入容器后可执行：

```bash
go build ./...
go test ./...
```
