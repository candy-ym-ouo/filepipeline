# Bug Reproduction

## 问题现象

仓储实例首次创建任务时会向尚未初始化的内存状态映射写入数据，触发 `assignment to entry in nil map`，使正常的首次写入直接崩溃。

## 复现方式

在项目根目录执行：

```bash
go test ./internal/repository -count=1 -run '^TestBug007_CreateTaskDoesNotPanicOnFirstUse$'
```

测试创建新的数据库和仓储实例，不执行任何预热操作，随后立即写入第一个任务。

## 基线结果

原始基线代码在第一次创建任务时向 nil map 写入，测试因运行时 panic 失败。

## 预期结果

仓储构造完成后内部状态可安全写入，首次创建任务不发生 panic，数据持久化成功且测试正常通过。
