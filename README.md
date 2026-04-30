# pgcacher

`pgcacher` 用于获取文件的 page cache 统计信息。使用 **pgcacher** 命令可以了解指定进程的 fd 在 page cache 中占用了多少缓存空间。使用 **pgcacher** 可以了解指定文件列表是否被缓存在 page cache 中，以及被缓存的空间大小。

相比 pcstat，`pgcacher` 修复了进程文件列表不正确的问题。以前仅通过 `/proc/{pid}/maps` 获取，现在改为同时从 `/proc/{pid}/maps` 和 `/proc/{pid}/fd` 获取。pgcacher 支持更多参数，例如 top、worker、limit、depth、least-size、exclude-files 和 include-files。😁

此外，pgcacher 的代码更加健壮，并且支持并发参数，可以更快速地计算 page cache 中的缓存占用情况。

🚀 pgcacher 比 pcstat 性能更好，并且随着文件数量的增加，性能差距会越来越明显。在大多数场景下，可达到 pcstat 的 5 倍速度。

> `pkg/pcstats` 中的部分代码改编自 pcstat 和 hcache。

## 使用说明

```sh
pgcacher <-json|-terse|-default> <-bname> file file file
    -limit 限制显示的文件数量，默认值：500
    -depth 设置扫描目录的深度，默认值：0
    -worker 并发 worker 数量，默认值：2
    -pid 显示指定 pid 打开的所有文件（同时读取 /proc/{pid}/fd 与 /proc/{pid}/maps）
    -container 要分析的容器 ID（>=12 位十六进制字符）；通过 /proc/<pid>/cgroup 解析为宿主机 PID，无需 docker/nsenter
    -top 扫描所有进程打开的文件，显示在 page cache 中占用内存空间最多的前几个文件，默认值：false
    -least-size 忽略小于 leastSize 的文件，例如 '10MB' 和 '15GB'
    -exclude-files 通过通配符排除指定文件，例如 'a*c?d' 和 '*xiaorui*,rfyiamcool'
    -include-files 通过通配符仅包含指定文件，例如 'a*c?d' 和 '*xiaorui?cc,rfyiamcool'
    -statblockdev 纳入进程持有的块设备（/dev/sdX、/dev/nvmeXnY、loop*、dm-* 等）的 page cache；默认关闭。开启后会对每个 /dev/* fd 执行 stat + BLKGETSIZE64 ioctl 以获取真实大小，可能显著变慢
    -enhanced-ns 启用增强型命名空间切换以获得更好的容器支持
    -verbose 启用详细日志以调试命名空间操作
    -json 以 JSON 格式输出
    -terse 打印简洁的机器可解析输出
    -bname 在输出中使用 basename(file)（适用于长路径）
    -plain 返回不带框线字符的数据
    -unicode 返回带 unicode 框线字符的数据
```

## 容器环境支持

`pgcacher` 为容器化环境提供了一流的支持。当分析运行在容器内的进程时，您有以下几种选择：

### 1. 内置 `-container` 参数（推荐）

无需 `docker`、`nsenter` 或包装脚本。`pgcacher` 通过扫描 `/proc/<pid>/cgroup` 将容器 ID 解析为宿主机 PID，并自动启用跨命名空间切换。

```bash
# 接受完整或截断的容器 ID（>=12 位十六进制字符），
# 支持 Docker、containerd、CRI-O、Podman 和 Kubernetes。
sudo pgcacher -container <container_id> -top -limit 10
```

### 2. 使用 nsenter

```bash
# 获取容器主进程在宿主机上的 PID
CONTAINER_PID=$(docker inspect --format '{{.State.Pid}}' <container_name>)

# 使用 nsenter 进入容器的 mount / pid 命名空间后运行 pgcacher；
# 进入容器 PID namespace 后，容器主进程对应 PID 为 1
sudo nsenter -t $CONTAINER_PID -m -p pgcacher -pid 1
```

### 3. 使用提供的脚本

```bash
# 赋予脚本可执行权限
chmod +x scripts/pgcacher-container.sh

# 通过容器名称运行
./scripts/pgcacher-container.sh -c <container_name>

# 通过容器 ID 运行
./scripts/pgcacher-container.sh -i <container_id>

# 通过指定 PID 和附加参数运行
./scripts/pgcacher-container.sh -p <pid> -a "-top -limit 10"
```

### 4. 增强型命名空间切换（实验性）

```bash
# 为已知的宿主机 PID 启用增强型命名空间切换
sudo pgcacher -pid <container_pid> -enhanced-ns -verbose
```

有关详细用法示例和故障排查，请参阅 [docs/container-usage.md](docs/container-usage.md)。

## 安装

目前暂未提供预编译二进制（后续考虑通过 GitHub Releases + GitHub Actions 发布），请自行从源码构建：

```sh
git clone https://github.com/ForceInjection/pgcacher.git
cd pgcacher
make build
sudo cp pgcacher /usr/local/bin/
pgcacher -h
```

`make build` 会交叉编译出 `linux/amd64` 二进制；如需本地平台构建（例如在 Linux 机器上直接编译）可改用 `go build .`。

## 示例

以下示例均在 Linux 服务器（CentOS 8.x，内核 4.18，Kubernetes + containerd 节点）上实际运行采集，非模拟数据。

### 准备测试文件

```bash
# 生成三个随机内容的文件并读入 page cache
mkdir -p demo
dd if=/dev/urandom of=demo/sample_256m bs=1M count=256
dd if=/dev/urandom of=demo/sample_128m bs=1M count=128
dd if=/dev/urandom of=demo/sample_64m  bs=1M count=64
cat demo/sample_256m demo/sample_128m demo/sample_64m > /dev/null
```

### 1. 文件模式（默认 ASCII 表格）

```text
$ sudo pgcacher demo/sample_256m demo/sample_128m demo/sample_64m
+------------------+----------------+-------------+----------------+-------------+---------+
| Name             | Size           │ Pages       │ Cached Size    │ Cached Pages│ Percent │
|------------------+----------------+-------------+----------------+-------------+---------|
| demo/sample_256m | 256.000M       | 65536       | 254.500M       | 65152       | 99.414  |
| demo/sample_128m | 128.000M       | 32768       | 128.000M       | 32768       | 100.000 |
| demo/sample_64m  | 64.000M        | 16384       | 64.000M        | 16384       | 100.000 |
|------------------+----------------+-------------+----------------+-------------+---------|
│ Sum              │ 448.000M       │ 114688      │ 446.500M       │ 114304      │ 99.665  │
+------------------+----------------+-------------+----------------+-------------+---------+
```

> 注意：`sample_256m` 的缓存占比是 99.414%，并非精确的 100%。这是真实环境下内存压力导致少量页面被回收的正常现象。

### 2. 目录递归扫描（-depth）

```text
$ sudo pgcacher -depth 2 -least-size 50MB demo/
+------------------+----------------+-------------+----------------+-------------+---------+
| Name             | Size           │ Pages       │ Cached Size    │ Cached Pages│ Percent │
|------------------+----------------+-------------+----------------+-------------+---------|
| demo/sample_256m | 256.000M       | 65536       | 254.500M       | 65152       | 99.414  |
| demo/sample_128m | 128.000M       | 32768       | 128.000M       | 32768       | 100.000 |
| demo/sample_64m  | 64.000M        | 16384       | 64.000M        | 16384       | 100.000 |
|------------------+----------------+-------------+----------------+-------------+---------|
│ Sum              │ 448.000M       │ 114688      │ 446.500M       │ 114304      │ 99.665  │
+------------------+----------------+-------------+----------------+-------------+---------+
```

### 3. 按 PID 查看进程打开的文件（-pid）

PID 1（systemd）是所有 Linux 主机都存在的进程，下例展示其动态链接库的缓存占比：

```text
$ sudo pgcacher -pid 1 -least-size 100KB -limit 8
+-------------------------------+----------------+-------------+----------------+-------------+---------+
| Name                          | Size           │ Pages       │ Cached Size    │ Cached Pages│ Percent │
|-------------------------------+----------------+-------------+----------------+-------------+---------|
| /usr/lib64/libc-2.28.so       | 17.411M        | 4458        | 3.999M         | 1024        | 22.970  |
| /usr/lib64/libpthread-2.28.so | 2.653M         | 680         | 2.653M         | 680         | 100.000 |
| /usr/lib64/ld-2.28.so         | 1.401M         | 359         | 1.401M         | 359         | 100.000 |
| /usr/lib/systemd/systemd      | 1.557M         | 399         | 1.190M         | 305         | 76.441  |
| /usr/lib64/libmount.so.1.1.0  | 271.297K       | 68          | 271.297K       | 68          | 100.000 |
| /usr/lib64/libblkid.so.1.1.0  | 259.352K       | 65          | 259.352K       | 65          | 100.000 |
| /usr/lib64/liblzma.so.5.2.2   | 153.750K       | 39          | 153.750K       | 39          | 100.000 |
| /usr/lib64/libselinux.so.1    | 152.094K       | 39          | 77.996K        | 20          | 51.282  |
|-------------------------------+----------------+-------------+----------------+-------------+---------|
│ Sum                           │ 23.840M        │ 6107        │ 9.989M         │ 2560        │ 41.919  │
+-------------------------------+----------------+-------------+----------------+-------------+---------+
```

> `libc-2.28.so` 仅有 22.97% 的页面驻留在缓存中，这与 glibc 通过 demand-paging 按需加载代码段的特性一致。

### 4. 容器模式（-container）

在 Kubernetes + containerd 节点上直接传入容器 ID，无需预先查询宿主机 PID：

```text
$ sudo pgcacher -container 75c1192030e92a808202ba8423fe82cb660ccf977d92453298336d0e2b734389 -verbose /etc/hostname
2026/04/30 13:33:41 container 75c1192030e9...d2b734389 -> host pid 720114
2026/04/30 13:33:41 Successfully switched to mnt namespace of pid 720114
+---------------+----------------+-------------+----------------+-------------+---------+
| Name          | Size           │ Pages       │ Cached Size    │ Cached Pages│ Percent │
|---------------+----------------+-------------+----------------+-------------+---------|
| /etc/hostname | 7B             | 1           | 0B             | 0           | 0.000   |
|---------------+----------------+-------------+----------------+-------------+---------|
│ Sum           │ 7B             │ 1           │ 0B             │ 0           │ 0.000   │
+---------------+----------------+-------------+----------------+-------------+---------+
```

> 容器 ID 通过扫描 `/proc/<pid>/cgroup` 解析为宿主机 PID（720114），随后自动切换到容器的 mount namespace 读取容器视角下的 `/etc/hostname`。

### 5. Unicode 框线输出（-unicode）

```text
$ sudo pgcacher -unicode demo/sample_256m demo/sample_128m demo/sample_64m
┌──────────────────┬────────────────┬─────────────┬────────────────┬─────────────┬─────────┐
│ Name             │ Size           │ Pages       │ Cached Size    │ Cached Pages│ Percent │
├──────────────────┼────────────────┼─────────────┼────────────────┼─────────────┼─────────┤
│ demo/sample_256m │ 256.000M       │ 65536       │ 214.402M       │ 54887       │ 83.751  │
│ demo/sample_128m │ 128.000M       │ 32768       │ 128.000M       │ 32768       │ 100.000 │
│ demo/sample_64m  │ 64.000M        │ 16384       │ 64.000M        │ 16384       │ 100.000 │
├──────────────────┼────────────────┼─────────────┼────────────────┼─────────────┼─────────┤
│ Sum              │ 448.000M       │ 114688      │ 406.402M       │ 104039      │ 90.715  │
└──────────────────┴────────────────┴─────────────┴────────────────┴─────────────┴─────────┘
```

### 6. 机器可解析格式（-json / -terse）

```text
$ sudo pgcacher -json demo/sample_256m demo/sample_128m demo/sample_64m
[{"filename":"demo/sample_256m","size":268435456,"timestamp":"2026-04-30T13:33:10.409+08:00","mtime":"2026-04-30T13:26:18.753+08:00","pages":65536,"cached":63591,"uncached":1945,"percent":97.032},{"filename":"demo/sample_128m","size":134217728,"timestamp":"2026-04-30T13:33:10.409+08:00","mtime":"2026-04-30T13:26:19.775+08:00","pages":32768,"cached":32768,"uncached":0,"percent":100},{"filename":"demo/sample_64m","size":67108864,"timestamp":"2026-04-30T13:33:10.416+08:00","mtime":"2026-04-30T13:26:20.288+08:00","pages":16384,"cached":16384,"uncached":0,"percent":100}]

$ sudo pgcacher -terse demo/sample_256m demo/sample_128m demo/sample_64m
name,size,timestamp,mtime,pages,cached,percent
demo/sample_256m,268435456,1777527190,1777526778,65536,61031,93.126
demo/sample_128m,134217728,1777527190,1777526779,32768,32768,100
demo/sample_64m,67108864,1777527190,1777526780,16384,16384,100
```

## pgcacher 设计

下面两张示意图来自原作者博客，概述了整体数据流与 `mincore` 探测机制：

![](https://xiaorui-cc.oss-cn-hangzhou.aliyuncs.com/images/202303/202303121052113.png)

![](https://xiaorui-cc.oss-cn-hangzhou.aliyuncs.com/images/202303/202303131739063.png)

架构、并发模型、容器/块设备支持等实现细节请参阅 [docs/design.md](docs/design.md)。

## 致谢

@tobert 提供的 pcstat
