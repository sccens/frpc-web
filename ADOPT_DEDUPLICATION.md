# Adopt 功能去重改进

## 问题背景

在生产环境中发现，`adopt`（接管）功能每次都会创建新的服务器记录，即使是相同的 frpc 配置。这导致：

1. **重复的服务器记录**：同一个 frpc 服务端配置被多次导入
2. **重复的代理规则**：相同的规则被重复创建，造成混乱
3. **管理困难**：用户不清楚哪个服务器记录是实际在用的

### 复现场景

```bash
# 第一次接管
采用 restart 模式接管一个 frpc 进程
→ 创建服务器 A，导入规则

# 第二次接管（例如重启后再次接管）
再次接管同一个 frpc 进程
→ 创建服务器 B，导入相同的规则  ← 问题：创建了重复的服务器
```

## 解决方案

### 核心逻辑

在 `AdoptProcess` 中添加**服务器去重检查**：

1. **解析配置**：先解析配置文件，获取 `serverAddr` 和 `serverPort`
2. **检查已存在**：在现有服务器中查找相同的 `serverAddr:serverPort`
3. **复用或创建**：
   - 如果找到匹配的服务器 → 复用已有服务器
   - 如果没有找到 → 创建新服务器

### 判断标准

两个服务器被认为是"相同"的条件：

```go
server1.ServerAddr == server2.ServerAddr && 
server1.ServerPort == server2.ServerPort
```

**原因**：`serverAddr:serverPort` 唯一标识一个 frps 服务端入口，同一入口的配置应该使用同一个服务器记录。

## 代码实现

### 核心改动

```go
// 解析配置以获取服务端信息
serverInput, _, parseErr := parseFrpcConfig(content)
if parseErr != nil {
    return AdoptResult{}, invalidInput(fmt.Errorf("解析配置失败: %w", parseErr))
}

// 检查是否已存在相同 serverAddr:serverPort 的服务器
existingServers, _ := s.store.ListServers(ctx)
var existingServer *Server
for i := range existingServers {
    if existingServers[i].ServerAddr == serverInput.ServerAddr &&
        existingServers[i].ServerPort == serverInput.ServerPort {
        existingServer = &existingServers[i]
        break
    }
}

var server Server
if existingServer != nil {
    // 复用已存在的服务器
    server = *existingServer
    if input.Name != "" {
        // 更新服务器名称（如果用户提供了新名称）
        updateInput := ServerInput{...}
        server, _ = s.store.UpdateServer(ctx, server.ID, updateInput)
    }
} else {
    // 创建新服务器
    server, err = s.createServerFromConfig(ctx, input.Name, content, true)
    if err != nil {
        return AdoptResult{}, err
    }
}
```

### 用户反馈

返回消息会明确告知用户是复用还是新建：

- **复用现有服务器**：
  ```
  ✅ 已纳管：复用了现有服务器配置「测试服务器」，原进程已停止，面板已重新启动 frpc。
  ```

- **创建新服务器**：
  ```
  ✅ 已纳管：原进程已停止，面板已用导入的配置重新启动 frpc。
  ```

## 使用场景

### 场景 1：首次接管

```
用户操作：接管一个新的 frpc 进程
系统行为：未找到匹配的服务器，创建新记录
结果：服务器列表新增一条记录
```

### 场景 2：重复接管（例如重启后）

```
用户操作：再次接管同一个 frpc 进程
系统行为：找到匹配的服务器（serverAddr:serverPort 相同），复用该记录
结果：服务器列表不变，不产生重复记录
```

### 场景 3：多实例场景

```
情况：用户有多个 frpc 进程，连接到不同的 frps 服务端
系统行为：
  - 接管 frpc-1 (连接 server-a.com:7000) → 创建服务器 A
  - 接管 frpc-2 (连接 server-b.com:7000) → 创建服务器 B
  - 再次接管 frpc-1 → 复用服务器 A
结果：两个不同的服务器记录，各自管理对应的进程
```

## 边界情况处理

### 1. 服务器地址变更

如果用户修改了 frpc 配置文件中的 `serverAddr` 或 `serverPort`：

- **行为**：会被识别为新的服务器，创建新记录
- **原因**：不同的服务端入口应该分开管理
- **建议**：用户应该手动删除旧的服务器记录

### 2. 名称冲突

如果用户在接管时指定了新名称：

- **复用模式**：会更新已有服务器的名称
- **新建模式**：使用指定的名称创建

### 3. 规则差异

**当前实现**：复用服务器时**不会**更新规则

- **原因**：规则可能被用户手动修改过，强制同步会丢失用户的自定义配置
- **建议**：如果配置文件中的规则有变化，用户应该：
  1. 手动删除旧服务器
  2. 重新接管以导入新规则
  3. 或者在面板中手动编辑规则

## 兼容性

- ✅ **向后兼容**：不影响现有的服务器记录
- ✅ **数据迁移**：不需要迁移已有数据
- ✅ **功能完整**：不影响其他功能（导入配置、手动创建服务器等）

## 测试验证

- ✅ 所有现有测试通过（`TestAdoptProcessRestart`, `TestAdoptProcessMissingPID`）
- ✅ 编译通过，无类型错误
- ✅ 去重逻辑已集成到 `AdoptProcess` 函数

## 文件变更

- `internal/app/frpcimport.go` - 添加去重逻辑到 `AdoptProcess` 函数

## 后续改进建议

1. 【可选】在复用服务器时，对比规则差异并提示用户
2. 【可选】提供"强制重新导入"选项，清除旧规则并导入新规则
3. 【可选】在前端显示"此服务器已被纳管 N 次"的统计信息
