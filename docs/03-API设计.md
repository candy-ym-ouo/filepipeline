# 03 · API 设计

> 本文档定义 HTTP API 的完整约定。基础路径 `/api/v1`，请求/响应均为 JSON（上传除外），
> 错误统一使用 `{"code": "...", "message": "..."}` 结构。

---

## 1. 通用约定

### 1.1 基础信息

| 项 | 值 |
| --- | --- |
| Base Path | `/api/v1` |
| 内容类型 | `application/json`（`POST /api/v1/files` 为 `multipart/form-data`） |
| 字符集 | UTF-8 |
| 时间格式 | RFC3339（如 `2026-01-01T12:00:00Z`） |
| 任务 ID | `t_` 前缀 + UUID 短码，如 `t_8f3a2b1c` |

### 1.2 错误响应结构

```json
{
  "code": "ERR_TASK_NOT_FOUND",
  "message": "任务不存在: t_8f3a2b1c"
}
```

### 1.3 错误码表

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `ERR_BAD_REQUEST` | 请求体/参数非法 |
| 400 | `ERR_INVALID_CALLBACK_SIGNATURE` | 回调签名校验失败 |
| 404 | `ERR_TASK_NOT_FOUND` | 任务不存在 |
| 404 | `ERR_NOT_FOUND` | 资源不存在（通用） |
| 409 | `ERR_TASK_NOT_RETRYABLE` | 任务状态不允许重试 |
| 409 | `ERR_TASK_ALREADY_FINAL` | 任务已处于终态，不可改写 |
| 413 | `ERR_FILE_TOO_LARGE` | 上传文件超过 10 MiB |
| 415 | `ERR_UNSUPPORTED_MEDIA` | 非 multipart 上传 |
| 422 | `ERR_VALIDATION_FAILED` | 文件校验未通过（返回校验失败详情） |
| 500 | `ERR_INTERNAL` | 服务内部错误 |

### 1.4 任务对象（Task）JSON 形态

```json
{
  "id": "t_8f3a2b1c",
  "filename": "report.txt",
  "size": 1024,
  "sha256": "9f86d081884c7d659a2feaa0c55ad015...",
  "status": "PENDING",
  "stage": "VALIDATE",
  "attempts": 0,
  "max_attempts": 3,
  "error_code": "",
  "error_message": "",
  "scan_verdict": "",
  "scan_scanner": "",
  "extracted_summary": "",
  "created_at": "2026-01-01T12:00:00Z",
  "updated_at": "2026-01-01T12:00:00Z"
}
```

## 2. 接口定义

### 2.1 `GET /healthz` — 存活检查

**响应 200**

```json
{ "status": "ok", "service": "file-pipeline", "time": "2026-01-01T12:00:00Z" }
```

### 2.2 `POST /api/v1/files` — 上传文件并创建任务

**请求**：`multipart/form-data`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `file` | 文件 | 是 | 待处理文件，≤ 10 MiB |
| `callback_url` | string | 否 | 业务方接收处理结果通知的 URL（预留） |
| `callback_token` | string | 否 | 回调签名密钥（异步扫描回调用） |

**成功 201**：返回创建的任务对象（见 1.4）。

**失败**：

| 场景 | HTTP | 示例 |
| --- | --- | --- |
| 无文件字段 | 400 | `{"code":"ERR_BAD_REQUEST","message":"缺少 file 字段"}` |
| 超过 10 MiB | 413 | `{"code":"ERR_FILE_TOO_LARGE","message":"文件超过 10MiB 限制"}` |
| 磁盘错误 | 500 | `{"code":"ERR_INTERNAL","message":"保存文件失败"}` |

**curl 示例**

```bash
curl -X POST http://127.0.0.1:8080/api/v1/files \
  -F "file=@report.txt" \
  -F "callback_token=secret"
```

### 2.3 `GET /api/v1/tasks/{id}` — 查询任务详情

**成功 200**：任务对象 + 阶段事件列表

```json
{
  "task": { "...": "见 1.4" },
  "events": [
    { "stage": "VALIDATE", "status": "SUCCEEDED", "attempt": 1,
      "message": "格式校验通过", "created_at": "2026-01-01T12:00:01Z" },
    { "stage": "EXTRACT", "status": "SUCCEEDED", "attempt": 1,
      "message": "提取 128 行文本", "created_at": "2026-01-01T12:00:02Z" }
  ]
}
```

**失败**：`404 {"code":"ERR_TASK_NOT_FOUND", ...}`

### 2.4 `GET /api/v1/tasks` — 任务列表

**查询参数**

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `status` | string | 全部 | `PENDING/PROCESSING/SUCCEEDED/FAILED/DEAD` |
| `page` | int | 1 | 页码（≥1） |
| `page_size` | int | 20 | 每页数量（1~100） |

**成功 200**

```json
{
  "items": [ { "task": "..." }, { "task": "..." } ],
  "total": 42,
  "page": 1,
  "page_size": 20
}
```

### 2.5 `POST /api/v1/tasks/{id}/retry` — 手动重试

**规则**：仅 `FAILED` 状态可重试；重试后 `attempts` 归零、状态回 `PENDING`、
从首次失败阶段重跑；手动重试累计 3 次仍失败 → `DEAD`。

**成功 200**

```json
{ "id": "t_8f3a2b1c", "status": "PENDING", "retry_count": 1 }
```

**失败**

| 场景 | HTTP | 说明 |
| --- | --- | --- |
| 任务不存在 | 404 | `ERR_TASK_NOT_FOUND` |
| 任务非 FAILED | 409 | `ERR_TASK_NOT_RETRYABLE`（SUCCEEDED/DEAD/PENDING/PROCESSING 均拒绝） |

### 2.6 `POST /api/v1/scan-callback` — 扫描结果异步回调

**请求头**：`X-Callback-Signature: <HMAC-SHA256(body, callback_token)>`

**请求体**

```json
{
  "task_id": "t_8f3a2b1c",
  "verdict": "clean",
  "scanner": "mock-antivirus",
  "scanned_at": "2026-01-01T12:05:00Z",
  "signature": "hex(hmac-sha256(raw_body, callback_token))"
}
```

**成功 200**

```json
{ "accepted": true, "task_id": "t_8f3a2b1c" }
```

**失败**

| 场景 | HTTP | 说明 |
| --- | --- | --- |
| 签名不合法 | 400 | `ERR_INVALID_CALLBACK_SIGNATURE` |
| 任务不存在 | 404 | `ERR_TASK_NOT_FOUND` |
| 任务不处于等待回调态 | 409 | `ERR_TASK_ALREADY_FINAL`（幂等忽略重复回调） |
| verdict 非法 | 400 | 仅允许 `clean/infected/error` |

**判定语义**：`clean` / `infected` → 任务进入 DONE → `SUCCEEDED`；
`error` → 任务回 `PENDING`（SCAN 阶段）触发重试。

### 2.7 `GET /` — 前端页面

返回 `web/index.html`，同时静态托管 `/static/*`（style.css、app.js）。

## 3. 接口与前端交互流程

```
前端页面
 ├─ 上传区  ──POST /api/v1/files──► 显示 task_id，轮询详情
 ├─ 列表区  ──GET /api/v1/tasks──► 渲染状态徽章 + 分页
 ├─ 详情区  ──GET /api/v1/tasks/:id──► 渲染阶段时间线
 └─ 重试按钮 ──POST /api/v1/tasks/:id/retry──► 刷新列表
```

## 4. 校验规则（服务端强制）

1. `page >= 1`、`1 <= page_size <= 100`，否则 400；
2. `status` 参数必须是合法枚举，否则 400；
3. 上传必须为 `multipart/form-data` 且包含 `file` 字段，否则 400/415；
4. 文件大小在读取前以 `Content-Length` 预检，超限直接 413（不落盘）；
5. 回调 `verdict` 白名单校验，非法值 400；
6. 回调体 `task_id` 必须存在且任务处于等待态，否则按 2.6 失败表处理。

## 5. 安全说明

- 上传文件落盘使用随机生成的文件名（`f_<uuid>.<ext>`），**不使用用户原始文件名**，
  杜绝路径穿越与目录注入；
- 原始文件名仅保存在数据库 `filename` 字段用于展示；
- 回调签名采用 HMAC-SHA256，密钥为任务创建时下发的 `callback_token`；
- 所有响应包含 `Content-Type: application/json; charset=utf-8`。
