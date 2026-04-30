# pgcacher 容器环境使用指南

本文档介绍如何在容器化环境中使用 pgcacher 工具来分析页面缓存。

## 1. 问题背景

在容器化环境中，由于命名空间隔离的存在，直接在宿主机上运行 pgcacher 可能会遇到以下问题：

- 文件路径不匹配：容器内的文件路径在宿主机上不存在
- 权限不足：无法访问容器的命名空间
- 进程可见性：无法正确获取容器内进程的文件描述符信息

## 2. 解决方案

### 2.0 方案零：内置 `-container` 标志（推荐，无外部依赖）

这是最简单的方式。pgcacher 会扫描 `/proc/*/cgroup` 自动把容器 ID 解析为宿主机 PID，然后自动启用增强命名空间切换。无需 `docker`、`nsenter` 或辅助脚本；支持 Docker、containerd、CRI-O、Podman、Kubernetes，兼容 cgroup v1 / v2。

```bash
# 只要提供容器 ID（至少 12 位十六进制）即可
./pgcacher -container abc123def456 -top

# 配合常用参数
sudo ./pgcacher -container abc123def456 -limit 100 -least-size 1MB -json

# verbose 模式可查看解析出的宿主机 PID
sudo ./pgcacher -container abc123def456 -verbose
```

限制：

- 只接受容器 **ID**，不接受容器 **名称**。名称解析需要与运行时 socket 通信（Docker / containerd API），由下面的方案一或方案二承担。
- 需要读取 `/proc/<pid>/cgroup` 的权限，通常意味着 root 或 `CAP_SYS_PTRACE`。
- `-container` 与 `-pid` 互斥。

### 2.1 方案一：使用 nsenter（备选）

这是最传统的做法，使用系统自带的 nsenter 工具：

```bash
# 1. 获取容器 ID
CONTAINER_ID=$(docker ps | grep your-app | awk '{print $1}')

# 2. 获取容器主进程 PID
CONTAINER_PID=$(docker inspect -f '{{.State.Pid}}' $CONTAINER_ID)

# 3. 获取容器内目标进程 PID（可选）
TARGET_PID=$(nsenter --target $CONTAINER_PID -p -r ps -ef | grep your-process | awk '{print $2}')

# 4. 使用 nsenter 运行 pgcacher
nsenter --target $CONTAINER_PID -p -m ./pgcacher -pid=${TARGET_PID:-1}
```

### 2.2 方案二：使用辅助脚本（支持容器名称）

当你只有容器**名称**、或者需要在老旧环境上工作时，可以使用辅助脚本，它会通过 `docker inspect` 解析名称并回退到 nsenter：

```bash
# 使用容器 ID
./scripts/pgcacher-container.sh -c abc123def456 -v

# 使用容器名称
./scripts/pgcacher-container.sh -n my-app-container -v

# 指定特定进程 PID
./scripts/pgcacher-container.sh -c abc123def456 -p 123 -v

# 传递额外参数给 pgcacher
./scripts/pgcacher-container.sh -c abc123def456 -a "-limit 100 -json"
```

### 2.3 方案三：手动使用增强命名空间切换

如果你已经知道容器主进程在宿主机上的 PID，可以直接传 `-pid` 并启用 `-enhanced-ns`（`-container` 标志本质上就是该模式加上自动 PID 解析）：

```bash
# 使用增强的命名空间切换
./pgcacher -pid=<container_main_pid> -enhanced-ns -verbose

# 或者通过辅助脚本使用
./scripts/pgcacher-container.sh -c abc123def456 -e -v
```

## 3. 详细使用示例

### 3.1 分析 Nginx 容器的页面缓存

```bash
# 启动一个 Nginx 容器
docker run -d --name nginx-test nginx:latest

# 方法 1：使用辅助脚本
./scripts/pgcacher-container.sh -n nginx-test -v

# 方法 2：手动使用 nsenter
CONTAINER_PID=$(docker inspect -f '{{.State.Pid}}' nginx-test)
nsenter --target $CONTAINER_PID -p -m ./pgcacher -pid=1 -verbose
```

### 3.2 分析 Java 应用容器的页面缓存

```bash
# 假设有一个运行 Java 应用的容器
CONTAINER_NAME="my-java-app"

# 获取 Java 进程的 PID
CONTAINER_PID=$(docker inspect -f '{{.State.Pid}}' $CONTAINER_NAME)
JAVA_PID=$(nsenter --target $CONTAINER_PID -p -r ps -ef | grep java | grep -v grep | awk '{print $2}')

# 分析 Java 进程的页面缓存
./scripts/pgcacher-container.sh -n $CONTAINER_NAME -p $JAVA_PID -v
```

### 3.3 批量分析多个容器

```bash
#!/bin/bash
# 批量分析脚本示例

for container in $(docker ps --format "{{.Names}}"); do
    echo "Analyzing container: $container"
    ./scripts/pgcacher-container.sh -n "$container" -a "-json -limit 50" > "${container}_cache_analysis.json"
    echo "Results saved to ${container}_cache_analysis.json"
done
```

## 4. 技术实现细节

### 4.1 命名空间切换原理

增强的命名空间切换功能通过以下步骤实现：

1. **检测命名空间差异**：比较当前进程和目标进程的命名空间 ID
2. **打开命名空间文件**：访问 `/proc/<pid>/ns/<namespace_type>` 文件
3. **执行 setns 系统调用**：切换到目标命名空间
4. **支持多种命名空间类型**：mount、pid、network、ipc、uts、user

### 4.2 错误处理和回退机制

```go
// 示例：增强命名空间切换的错误处理
if pg.option.enhancedNs {
    if err := pcstats.SwitchToContainerContext(pg.option.pid, pg.option.verbose); err != nil {
        if pg.option.verbose {
            log.Printf("Enhanced namespace switching failed, falling back to basic mode: %v", err)
        }
        // 回退到基本的挂载命名空间切换
        pcstats.SwitchMountNs(pg.option.pid)
    }
}
```

### 4.3 权限要求

- **nsenter 方式**：需要 root 权限或 CAP_SYS_ADMIN 能力
- **增强命名空间切换**：需要 CAP_SYS_ADMIN 能力
- **基本命名空间切换**：需要读取 `/proc/<pid>/ns/` 的权限

## 5. 故障排除

### 5.1 常见错误及解决方案

**错误："no such file or directory"**:

```bash
# 原因：在宿主机命名空间中访问容器文件路径
# 解决：使用 nsenter 或增强命名空间切换
./scripts/pgcacher-container.sh -c <container_id> -v
```

**错误："permission denied"**:

```bash
# 原因：权限不足
# 解决：使用 sudo 运行
sudo ./scripts/pgcacher-container.sh -c <container_id> -v
```

**错误："failed to switch namespace"**:

```bash
# 原因：命名空间切换失败
# 解决：检查容器是否运行，使用 verbose 模式查看详细错误
./pgcacher -pid=<pid> -enhanced-ns -verbose
```

### 5.2 调试技巧

```bash
# 1. 检查容器状态
docker ps -a

# 2. 检查容器进程
docker top <container_name>

# 3. 检查命名空间
ls -la /proc/<container_pid>/ns/

# 4. 使用 verbose 模式
./pgcacher -pid=<pid> -enhanced-ns -verbose
```

## 6. 性能考虑

### 6.1 命名空间切换开销

- **nsenter 方式**：每次调用都需要创建新进程，开销较大
- **增强命名空间切换**：在同一进程内切换，开销较小
- **建议**：对于一次性分析使用 nsenter，对于频繁分析使用增强模式

### 6.2 内存使用优化

```bash
# 限制分析的文件数量
./pgcacher -pid=<pid> -enhanced-ns -limit 100

# 过滤小文件
./pgcacher -pid=<pid> -enhanced-ns -least-size 1MB

# 使用 terse 输出减少内存使用
./pgcacher -pid=<pid> -enhanced-ns -terse
```

## 7. 最佳实践

1. **优先使用 nsenter 方式**：最稳定可靠
2. **使用辅助脚本**：简化操作流程
3. **启用 verbose 模式**：便于调试问题
4. **合理设置限制**：避免分析过多文件
5. **定期清理结果**：避免磁盘空间不足

```bash
# 推荐的使用模式
./scripts/pgcacher-container.sh -n <container_name> -v -a "-limit 200 -least-size 1MB"
```
