# 对话内容记录功能

## 需求背景

系统在用户每次调用 API 后，会记录一条消耗日志（`logs` 表），包含花费的 token 数量、模型名称、配额消耗等信息。但无法查看用户实际发送的对话内容（messages），这给 prompt 调试带来不便。

本功能在现有日志系统基础上，新增对话内容记录与查看能力，支持选择性开启，并可在日志设置中按时间清除对话记录。

## 变更内容

### 1. 数据库

**新增表：`conversation_records`**

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | INTEGER (主键, 自增) | 记录 ID |
| `log_id` | INTEGER (索引) | 关联的日志 ID（`logs.id`） |
| `user_id` | INTEGER (索引) | 用户 ID |
| `request_id` | VARCHAR(64) (索引) | 请求 ID |
| `content` | TEXT | 请求体原文（JSON 字符串） |
| `created_at` | BIGINT | 创建时间戳 |

该表存储在日志数据库（`LOG_DB`）中，若未配置独立日志数据库则与主数据库相同。

### 2. 后端

#### 2.1 新增全局配置 `ConversationRecordEnabled`

- 存储位置：`options` 表，key 为 `ConversationRecordEnabled`
- 默认值：`false`（关闭）
- 运行时变量：`common.ConversationRecordEnabled`

相关文件：
- `common/constants.go` — 变量定义
- `model/option.go` — OptionMap 初始化与 updateOptionMap 分发
- `model/log_conversation.go` — 模型与 CRUD 函数
- `model/log.go` — `RecordConsumeLog` 中新增记录逻辑
- `model/main.go` — `migrateDB` / `migrateLOGDB` / `migrateDBFast` 新增 `ConversationRecord{}`

#### 2.2 记录时机

在 `RecordConsumeLog` 函数中，日志成功写入后，若 `ConversationRecordEnabled` 为 `true`，则从当前请求的 gin context 中读取缓存的请求体（`BodyStorage`），并写入 `conversation_records` 表。

仅记录消费类型（type=2）的日志对应的对话内容。失败（error）或非 API 调用日志不记录。

#### 2.3 新增 API 接口

所有接口路径均在 `/api/log/` 下。

##### `GET /api/log/conversation/:log_id`

- 权限：`AdminAuth`
- 描述：根据日志 ID 查询对话内容
- 请求参数：路径参数 `log_id`（整数）
- 成功响应：
  ```json
  {
    "success": true,
    "data": {
      "id": 1,
      "log_id": 100,
      "user_id": 5,
      "request_id": "req-xxx",
      "content": "{\"messages\": [...]}",
      "created_at": 1712345678
    }
  }
  ```
- 错误响应：无匹配记录或参数无效时返回 `success: false` 及错误信息

##### `DELETE /api/log/conversation/`

- 权限：`AdminAuth`
- 描述：删除指定时间之前的对话记录
- 请求参数：query 参数 `target_timestamp`（Unix 秒级时间戳）
- 成功响应（无删除内容）：
  ```json
  {
    "success": true,
    "data": 42
  }
  ```
  `data` 为删除的记录总数，为 0 表示无需清除。

相关文件：
- `controller/log.go` — `GetConversationByLogId` / `DeleteConversationRecords`
- `router/api-router.go` — 路由注册

### 3. 前端

#### 3.1 日志设置（系统设置 → 运营设置 → 日志设置）

- 新增开关：**启用对话消息记录**
  - 对应配置项 `ConversationRecordEnabled`
  - 与「启用额度消费日志记录」开关并列放置
- 新增清除功能：**清除对话记录**
  - 与「清除历史日志」功能并列，单独选择日期时间
  - 清除选定时间之前的所有对话记录（不影响日志表）
  
相关文件：`web/src/pages/Setting/Operation/SettingsLog.jsx`

#### 3.2 日志表格

- 在消费类型日志的展开详情中新增 **「对话详情」** 行，点击「查看」链接触发弹窗
- 仅管理员可见，在展开详情的「计费模式」下方显示
- 点击后通过 API 查询并弹窗展示对话内容（JSON 格式化显示）
- 若无对话记录则显示提示文字

相关文件：
- `web/src/components/table/usage-logs/index.jsx` — Modal 渲染
- `web/src/components/table/usage-logs/modals/ConversationModal.jsx` — 弹窗组件
- `web/src/hooks/usage-logs/useUsageLogsData.jsx` — 展开详情条目 + 查询逻辑

## 设计原则

- **最小侵入**：所有修改集中在日志模块，未改动 relay 通道、计费等核心逻辑
- **可选开启**：默认关闭，不影响现有系统行为
- **独立清除**：对话记录清除与历史日志清除分开，数据生命周期独立管理
- **复用现有基础设施**：BodyStorage 请求体缓存机制、LOG_DB 日志数据库、OptionMap 配置系统
