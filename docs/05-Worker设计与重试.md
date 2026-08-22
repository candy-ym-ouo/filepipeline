# 05 · Worker 设计与重试机制

> 本文档定义后台 Worker 的完整设计：轮询调度、原子领取、阶段执行、退避重试、
> 异步回调等待与回收、幂等性、优雅关闭。与文档 01（业务逻辑）、04（数据模型）配套。

---

## 1. Worker 总体结构

```
┌──────────────────────────── cmd/worker ───────────────────────────┐
│ main：加载配置 → 执行迁移 → 装配仓库/存储/服务 → 启动 Worker → 等待信号 │
│                                                                    │
│ internal/worker.Worker                                             │
│  ├─ 轮询循环（goroutine 1）：                                      │
│  │   每 poll_interval 领取 batch 个任务，分发到处理协程 channel      │
│  ├─ 处理协程 ×N（默认 4）：                                        │
│  │   goroutine 2..N：从 channel 取任务 → Processor.Process          │
│  └─ 回收循环（goroutine N+1）：                                    │
│       周期 ReclaimExpired：PROCESSING 且租约过期 → 回滚 PENDING      │
└────────────────────────────────────────────────────────────────────┘
```

## 2. 轮询与领取

### 2.1 领取查询

```sql
SELECT * FROM tasks
 WHERE status = 'PENDING' AND next_run_at <= ?
 ORDER BY created_at ASC
 LIMIT ?;
```

### 2.2 原子领取（乐观锁）

对查询到的每个任务执行：

```sql
UPDATE tasks
   SET status = 'PROCESSING',
       version = version + 1,
       leased_until = ?,
       updated_at = ?
 WHERE id = ? AND status = 'PENDING' AND version = ?;
```

- 受影响行数 = 1 → 领取成功，交给处理协程；
- 受影响行数 = 0 → 已被其他协程/进程领取，跳过（不阻塞）。

### 2.3 调度参数

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `poll_interval` | 500ms | 轮询间隔 |
| `batch_size` | 8 | 每轮最多领取数 |
| `worker_count` | 4 | 处理协程数 |
| `lease_seconds` | 30 | 领取后租约时长 |

## 3. 阶段执行（Processor）

处理协程对单个任务**串行**执行阶段链，伪代码：

```
Process(task):
  for stage in task.Stage .. DONE:          # 从当前阶段开始
    switch stage:
      VALIDATE → ok, err := validator.Validate(task)
      EXTRACT  → ok, err := extractor.Extract(task)
      SCAN     → ok, err := scanner.Scan(task)   # 同步 or 异步提交
      DONE     → ok, err := pipeline.Finish(task)
    if ok:
      repo.CompleteStage(task, nextStage)        # 事务：推进 + 事件
      task = repo.GetTask(task.ID)               # 重读最新状态
    else:
      handleFailure(task, stage, err)            # 见第 4 节
      return
```

### 3.1 阶段成功

`CompleteStage` 在**同一 SQL 事务**内完成：

1. 校验版本号仍匹配（防并发改动）；
2. 推进 `stage`（VALIDATE→EXTRACT→SCAN→DONE）；
3. 若进入 DONE：`status=SUCCEEDED`（终态）；
4. 写入 `task_events`（stage, SUCCEEDED, attempt, message）。

### 3.2 阶段失败

`handleFailure` 决策（对应文档 01 · 5.4）：

```
if task.Attempts+1 < task.MaxAttempts:      # 还有重试机会
    backoff = RetryPolicy.Delay(attempt)    # 指数退避 + 抖动
    repo.FailStage(task, nextRunAt=now+backoff)
    # 事务：attempts+1, status=PENDING, 写事件(RETRYING/FAILED)
else:                                       # 重试耗尽
    repo.FailStageFinal(task, err)
    # 事务：status=FAILED, error_code/message, 写事件(FAILED)
```

## 4. 重试策略（RetryPolicy）

### 4.1 退避算法

```
delay(n) = min(base * 2^(n-1), maxBackoff) * (1 + jitter(±20%))
```

| 尝试次数 n | 理论等待 | 加抖动后区间 |
| --- | --- | --- |
| 1（首次失败） | 1s | 0.8 ~ 1.2s |
| 2 | 2s | 1.6 ~ 2.4s |
| 3 | 4s | 3.2 ~ 4.8s |
| ≥4 | 30s（封顶） | 24 ~ 36s |

### 4.2 重试语义

- **自动重试**：仅重跑**失败阶段**，成功阶段不重复执行；
- **手动重试**（`POST /api/v1/tasks/{id}/retry`）：
  - 前置：`status=FAILED`，否则 409；
  - 效果：`status=PENDING`、`attempts=0`、`retry_count+1`、
    从首次失败阶段重跑；
  - 上限：`retry_count >= 3` 时再失败 → `status=DEAD`（死信，不再流转）。

### 4.3 幂等保证

| 阶段 | 幂等机制 |
| --- | --- |
| VALIDATE | 只读文件 + 重算哈希，天然幂等 |
| EXTRACT | 只读解析，重跑结果一致；摘要覆盖写 |
| SCAN（同步） | 以 `task_id` 为幂等键：扫描服务对重复请求返回缓存结果 |
| SCAN（异步） | `waiting_callback=1` 期间忽略重复回调；回调超时重投扫描请求 |
| DONE | 终态守卫：`UPDATE ... WHERE status='PROCESSING'`，重复执行 0 行受影响 |

## 5. 异步回调等待与回收

### 5.1 等待态

- 扫描服务接受异步模式后，任务置 `waiting_callback=1` 并回 `PENDING`
  （`next_run_at = now + callback_wait(30s)`）；
- Worker 轮询时跳过 `waiting_callback=1` 的任务（仍为 PENDING 但等待回调，
  由回调 API 负责唤醒）。

### 5.2 回调到达

`POST /api/v1/scan-callback` 流程（API 进程）：

```
1. 校验 X-Callback-Signature（HMAC-SHA256(rawBody, callback_token)）→ 400
2. 解析 body，校验 verdict ∈ {clean, infected, error}
3. 查询任务：
   - 不存在 → 404
   - waiting_callback != 1 → 409（重复/过期回调，幂等忽略）
4. 事务：写 scan_verdict/scanner/scanned_at，waiting_callback=0，
   status=PENDING，stage=SCAN→DONE（verdict=error 时回到 SCAN 重试）
5. 返回 200 {accepted: true}
```

### 5.3 回调超时回收

- Worker 回收循环每 5s 执行：`UPDATE tasks SET waiting_callback=0,
  status='PENDING', next_run_at=now WHERE waiting_callback=1 AND next_run_at<=now`
  （回调 30s 未到 → 任务重新可领取 → 重投扫描请求，实现最终一致）。

## 6. 崩溃恢复（Lease 回收）

- 领取时写入 `leased_until = now + 30s`；
- Worker 启动时 + 每 10s 执行：

```sql
UPDATE tasks SET status = 'PENDING', leased_until = ''
 WHERE status = 'PROCESSING' AND leased_until < ?;
```

- 效果：进程崩溃后，其处理中的任务在租约过期后自动回到可领取状态，
  由其他 Worker/协程续跑（`attempts` 不变，重跑当前阶段，幂等安全）。

## 7. 优雅关闭

```
收到 SIGTERM / SIGINT
  → ctx.Cancel()
  → 轮询循环停止领取新任务
  → 等待处理协程完成当前阶段（超时 10s）
  → 超时未完成任务：回滚为 PENDING（状态 + lease 清零）
  → 关闭数据库连接 → 退出码 0
```

## 8. 可观测性

| 输出 | 内容 |
| --- | --- |
| 日志 | `[worker] task=t_xxx stage=VALIDATE result=ok attempt=1` |
| 日志 | `[worker] task=t_xxx stage=SCAN result=fail attempt=2 retry_in=2.3s err=...` |
| 事件表 | 全部阶段流转（前端时间线 + 排障依据） |
| 健康检查 | 通过 `GET /healthz` 仅暴露 API 进程存活；Worker 存活依赖日志与 DB 心跳（可选扩展） |

## 9. 与模块的对应关系

| Worker 能力 | 实现位置 |
| --- | --- |
| 轮询/领取/回收 | `internal/worker/worker.go` |
| 阶段执行循环 | `internal/worker/processor.go` |
| 校验/提取/扫描/编排 | `internal/service/*.go` |
| 退避策略 | `internal/service/retry.go` |
| 领取/推进/失败 SQL | `internal/repository/*.go` |
| 租约与状态常量 | `internal/domain/*.go` |
