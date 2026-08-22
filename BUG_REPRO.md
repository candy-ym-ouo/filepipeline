# Bug Reproduction

## 问题现象

扫描器返回“异步等待”状态时，阶段结果允许为空。流水线仍然直接解引用该结果，导致异步扫描路径发生 nil 指针运行时崩溃。

## 复现方式

在项目根目录执行：

```bash
go test ./internal/service -count=1 -run '^TestBug004_AsyncScanDoesNotDerefNilResult$'
```

测试使用异步扫描模式执行扫描阶段，并捕获可能出现的 panic。

## 基线结果

原始基线代码在处理空阶段结果时触发 nil 指针解引用，测试报告 panic。

## 预期结果

流水线先处理异步等待状态，不访问空结果；返回值正确标记为等待中，测试正常通过。
