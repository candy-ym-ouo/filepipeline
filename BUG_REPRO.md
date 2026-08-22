# Bug Reproduction

## 问题现象

两个扫描任务并发执行时，扫描阶段使用共享的任务标识保存结果。后启动的任务会覆盖先启动任务的标识，使扫描结果写入错误任务，造成跨任务状态污染，并可能触发数据竞争。

## 复现方式

在项目根目录执行：

```bash
go test ./internal/service -count=1 -run '^TestBug001_ConcurrentScanKeepsTaskIdentity$'
```

测试会让两个扫描请求同时进入服务端处理，以稳定触发任务标识被并发覆盖的执行路径。

## 基线结果

测试在原始基线代码上失败；使用竞态检测运行时还可能报告共享任务标识的数据竞争：

```bash
go test -race ./internal/service -count=1 -run '^TestBug001_ConcurrentScanKeepsTaskIdentity$'
```

## 预期结果

每次扫描都只使用当前调用对应的任务标识，两个并发任务互不影响，测试正常通过且不产生数据竞争。
