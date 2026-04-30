# pgcacher 设计文档

本文档介绍 `pgcacher` 的总体架构、核心流程与关键技术点，作为代码阅读和二次开发的参考。相关示意图来自原作者 [xiaorui.cc](https://xiaorui.cc/) 的博客，本文在其基础上结合当前仓库的实现进行展开。

---

## 1. 设计目标

`pgcacher` 是一个运行在 Linux 上的命令行工具，用于查询：

- **某个文件**在内核 page cache 中命中的字节数与页数；
- **某个进程**打开的所有常规文件的 page cache 占用情况；
- **全机范围**内 page cache 占用最多的文件（top 模式）。

相比早期的 `pcstat` / `hcache`，`pgcacher` 重点解决以下问题：

1. 进程文件列表不完整：同时扫描 `/proc/{pid}/fd` 与 `/proc/{pid}/maps`。
2. 串行 IO 过慢：引入 worker 池并发执行 mincore 探测。
3. 容器/命名空间场景不可用：内建 `-container` 标志，自动解析容器 ID → 宿主机 PID 并切换 mount / pid 命名空间，无需 `docker`、`nsenter` 或包装脚本。
4. 块设备 page cache 缺失：通过 `-statblockdev` 配合 `BLKGETSIZE64 ioctl` 将 `/dev/sdX`、`/dev/nvmeXnY`、`loop*`、`dm-*` 等块设备纳入统计。

---

## 2. 参考示意图

下面两张图来自 pgcacher 原作者博客，分别展示了整体数据流与核心 mincore 机制，是理解本项目最快的入口。

### 2.1 整体数据流

![pgcacher 整体数据流](https://xiaorui-cc.oss-cn-hangzhou.aliyuncs.com/images/202303/202303121052113.png)

- CLI 解析 flag，选择一个或多个数据来源：文件/目录参数、`-pid`、`-container`。`-top` 与上述全部互斥，命中后直接扫描 `/proc` 所有进程并提前退出。
- 进程来源通过 `/proc/{pid}/fd` 与 `/proc/{pid}/maps` 聚合目标文件集合。
- 每个目标文件被送入 worker pool，并发调用 `mincore(2)` 获取 page cache 分布。
- 结果汇总为 `PcStatusList`，按缓存页数降序输出（ASCII/Unicode/plain 表格、JSON、terse CSV）。

### 2.2 mincore 探测原理

![mincore 探测原理](https://xiaorui-cc.oss-cn-hangzhou.aliyuncs.com/images/202303/202303131739063.png)

- 以 `PROT_NONE + MAP_SHARED` 将目标文件映射到进程地址空间；
- 调用 `mincore(2)` 得到每页 1 字节的命中向量，LSB 为 1 即命中 page cache；
- 统计向量即可得到 Cached / Miss 两个计数器；
- `PROT_NONE` 保证不会真正读入页数据，只是向内核查询驻留情况，开销非常低。

---

## 3. 运行模式

```text
┌──────────────────────────────────────────────────────────────┐
│ main.go: flag.Parse() + runtime.GOOS check (Linux-only)      │
└──────────────────┬──────────────────────┬────────────────────┘
                   │                      │
         ┌─────────▼────────┐   ┌─────────▼────────────────┐
         │ -container <id>  │   │ -pid <n> / file args /   │
         │  → 解析为宿主机 PID│   │    -top                  │
         │  → 自动开启       │   └─────────┬────────────────┘
         │    -enhanced-ns  │             │
         └─────────┬────────┘             │
                   │                      │
                   ▼                      ▼
         ┌───────────────────────────────────────────┐
         │ pgcacher.handleTop() / appendProcessFiles │
         │   + walkDirs(depth) + filterFiles         │
         └─────────────────────┬─────────────────────┘
                               │
                               ▼
         ┌───────────────────────────────────────────┐
         │ getPageCacheStats → pcstats.GetPcStatus   │
         │     ↳ mmap + mincore(2)                   │
         └─────────────────────┬─────────────────────┘
                               ▼
         ┌───────────────────────────────────────────┐
         │ output: ASCII | unicode | plain | JSON    │
         │         | terse CSV                       │
         └───────────────────────────────────────────┘
```

四种数据来源（中间三种可以组合使用，`-top` 与其它互斥）：

| 来源     | 触发方式            | 关键函数                                                  | 与其它的关系                           |
| -------- | ------------------- | --------------------------------------------------------- | -------------------------------------- |
| 文件来源 | 直接传文件/目录参数 | `walkDirs` + `getPageCacheStats`                          | 可与 `-pid` / `-container` 叠加        |
| 进程来源 | `-pid <n>`          | `appendProcessFiles` → `getProcessFiles`                  | 可与文件来源叠加；与 `-container` 互斥 |
| 容器来源 | `-container <id>`   | `pcstats.ResolveContainerPID` → 进程模式 + `-enhanced-ns` | 内部写回 `pid`；与 `-pid` 互斥         |
| Top      | `-top`              | `handleTop` 扫描 `/proc` 全部进程                         | 命中后 `os.Exit(0)`，与其它全部互斥    |

> 关于上面的 ASCII 示意图：右侧 `-pid / file args / -top` 框实际代表三条并行分支——`-top` 在其中优先被检测，命中后直接 `handleTop() + os.Exit(0)`；否则 `-pid` 触发 `appendProcessFiles`，随后与文件参数一同进入 `filterFiles` 和 `getPageCacheStats`。

---

## 4. 进程文件发现

`readProcessFiles` 同时读取两个来源，互为补充：

1. **`/proc/{pid}/fd/*`**：解析软链接得到真实路径。
   - 跳过 socket / pipe（不以 `/` 开头）。
   - 默认跳过 `/dev/*`；开启 `-statblockdev` 后仅保留块设备，字符设备（tty、`/dev/null` 等）仍然跳过，因为它们没有 page cache。
   - 判定块设备使用 `pcstats.IsBlockDevice`：`mode&ModeDevice != 0 && mode&ModeCharDevice == 0`。
2. **`/proc/{pid}/maps`**：扫描内存映射段，取第 6 列且以 `/` 开头的条目。覆盖通过 `mmap` 打开但未体现在 `fd` 中的文件（例如 JVM、动态库）。

两个列表合并后交给 `filterFiles` 去重并应用 `-include-files` / `-exclude-files` 通配符。

---

## 5. 并发与线程模型

为了在大机器上快速完成扫描，`pgcacher` 大量使用 worker pool：

- `-worker N`（默认 2）控制 goroutine 数量。
- 任务通过 buffered channel 投递，goroutine 从 channel 中消费，互斥追加结果。

### 5.1 Fast path vs Slow path

`getProcessFiles` 根据是否需要跨命名空间自适应选择路径：

- **Fast path**：未启用 `-enhanced-ns` 且 `pcstats.SameMountNamespace(pid)` 返回 true，直接在调用者 goroutine 中读取 `/proc`，无任何 setns 开销。Top 模式在非容器宿主机上几乎全部走这条路径。
- **Slow path**：需要进入容器命名空间。启动一个**一次性 goroutine**，在其中 `runtime.LockOSThread()`，然后执行 setns、读取 `/proc`，最后通过 channel 回传结果。**故意不调用 `UnlockOSThread`**：goroutine 结束时 Go runtime 会销毁这条已被污染的 OS 线程，避免后续不相关的 goroutine 继承到容器的 fs/mnt 视图。

### 5.2 setns(CLONE_NEWNS) 的陷阱

Linux 内核要求：调用 `setns(CLONE_NEWNS)` 的线程必须拥有独立的 `fs_struct`，否则返回 `EINVAL`。Go runtime 创建工作线程时默认使用 `CLONE_FS`，因此 `pkg/pcstats/mnt_ns_linux.go` 的 `setns` 封装在真正 setns 之前先执行一次 `unshare(CLONE_FS)`。这一步是容器支持能在 Go 程序里稳定工作的关键前提。

---

## 6. 容器支持

### 6.1 `-container` 解析

`pcstats.ResolveContainerPID(id)` 扫描 `/proc/*/cgroup`：

- 兼容 cgroup v1（`cpu:/docker/<id>`）与 v2（`/sys/fs/cgroup/.../<id>/...`）路径。
- 至少匹配前 12 位十六进制即认为命中，支持 Docker / containerd / CRI-O / Podman / Kubernetes。
- 解析成功后写回 `globalOption.pid` 并自动开启 `-enhanced-ns`；`-container` 与显式 `-pid` 互斥。

### 6.2 增强命名空间切换

`pcstats.SwitchToContainerContext(pid, verbose)` 同时切换：

- `mnt` 命名空间：使容器内的文件路径（如 `/var/log/app.log`）在宿主机进程中可解析。
- `pid` 命名空间：使 `/proc/self` 等视图指向容器内的进程树（调试容器进程时必需）。

失败时自动回退到仅切换 mount 命名空间的基础模式，保证可用性。

---

## 7. 块设备（`-statblockdev`）

块设备上的 page cache 对应裸设备 I/O（数据库原始设备、备份工具直接读 `/dev/sdX` 等场景）。默认关闭，开启后流程为：

1. `/proc/{pid}/fd` 扫描保留块设备链接；
2. `pcstats.GetPcStatus` 检测到 `ModeDevice && !ModeCharDevice` 后调用 `getBlockDeviceSize`；
3. `getBlockDeviceSize` 通过 `ioctl(BLKGETSIZE64)` 拿到真实字节数（fstat 对块设备会返回 0）；
4. 用设备真实大小执行 mmap + mincore。

ioctl 实现放在 `pkg/pcstats/blockdev_linux.go`（`//go:build linux`），非 Linux 平台使用 `blockdev_stub.go` 返回错误，既满足跨平台编译，又避免把 `BLKGETSIZE64 = 0x80081272` 这种 Linux 专属常量暴露到 darwin/bsd 构建里。

---

## 8. 包结构

```text
pgcacher/
├── main.go          # CLI 入口、flag 注册、模式分发
├── pgcacher.go      # pgcacher 结构体、getProcessFiles、worker pool
├── formats.go       # 5 种输出格式（ASCII / Unicode / plain / JSON / terse）
├── pgcacher_test.go # 根包单元测试（walkDirs、filterFiles、wildcardMatch）
├── formats_test.go  # 各输出格式的回归测试
├── cmd/
│   └── test-enhanced-ns/main.go   # 独立的容器命名空间切换诊断小工具
├── pkg/
│   ├── pcstats/
│   │   ├── mincore.go            # mmap + SYS_MINCORE 核心
│   │   ├── pcstatus.go           # GetPcStatus + IsBlockDevice
│   │   ├── blockdev_linux.go     # BLKGETSIZE64 ioctl（Linux-only）
│   │   ├── blockdev_stub.go      # 非 Linux 桩实现
│   │   ├── mnt_ns_linux.go       # mount 命名空间切换 + unshare(CLONE_FS)
│   │   ├── mnt_ns_unix.go        # 非 Linux 桩实现
│   │   ├── enhanced_ns.go        # mnt + pid 多命名空间切换
│   │   ├── enhanced_ns_stub.go   # 非 Linux 桩实现
│   │   ├── container_linux.go    # /proc/*/cgroup → 宿主 PID 解析
│   │   └── *_test.go             # container / enhanced_ns / pcstats 单元测试
│   └── psutils/
│       ├── scan.go               # 遍历 /proc，解析 /proc/{pid}/stat
│       ├── process.go            # Process 接口与按 RSS 排序
│       ├── refresh_linux.go      # Linux 进程 stat 刷新
│       ├── refresh_darwin.go     # darwin 桩实现
│       └── psutils_test.go       # 单元测试
├── scripts/         # 容器辅助脚本（demo.sh、pgcacher-container.sh、remote-test.sh）
└── docs/            # 使用与设计文档（container-usage.md、design.md）
```

核心 runtime 只运行在 Linux 上（`main.go` 在其他平台会 `log.Fatalf` 退出）；但所有桩文件保证 `go build .` / `go test ./...` 在 macOS、BSD 上依然可以用于本地开发与单元测试。

---

## 9. 输出格式

`PcStatusList` 按缓存页数降序排序后，由 `formats.go` 中的 5 个函数之一渲染：

| Flag       | 格式             | 适用场景           |
| ---------- | ---------------- | ------------------ |
| 默认       | ASCII 表格       | 交互终端查看       |
| `-unicode` | Unicode 框线表格 | 现代终端美观输出   |
| `-plain`   | 无框线纯文本     | 拷贝到其他文档     |
| `-json`    | 单行 JSON 数组   | 程序化消费         |
| `-terse`   | CSV 风格         | awk/Excel 快速处理 |

---

## 10. 参考文献

- 原作者博客：<https://xiaorui.cc/>
- 原版工具：[tobert/pcstat](https://github.com/tobert/pcstat)、[silenceshell/hcache](https://github.com/silenceshell/hcache)
- 内核接口：`mincore(2)`、`setns(2)`、`unshare(2)`、`ioctl_blkpg(2)`
- 相关文档：[docs/container-usage.md](container-usage.md)、项目根目录 [README.md](../README.md)
