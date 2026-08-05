# Features 二开特性

本文档记录在 new-api 上游代码基础上做的个性化功能修改。所有修改遵循**最小侵入 / 低冲突补丁**原则：

1. **优先新文件**：业务逻辑尽量放在独立文件，避免改上游热点文件中部。
2. **init 自注册**：渠道、API 类型、Adaptor、日志钩子通过 `Register*` 注册，合并时不必反复改 switch / 枚举尾部。
3. **高位私有 ID**：个人渠道类型使用 `1000+`，避开上游 `ChannelTypeDummy` 前的连续编号（避免与 `NewAPI=60` 等冲突）。
4. **稳定挂钩点**：上游仅保留极少、语义稳定的调用点（如 `runAfterConsumeLogHooks`、`GetRegisteredAdaptor`）。

---

## 架构：补丁注册面

| 注册点 | 文件 | 用途 |
|--------|------|------|
| `constant.ChannelTypeEcho` / `APITypeEcho` | `constant/personal.go` | 个人渠道常量（1000），`init` 写入 `ChannelTypeNames` |
| `channel.RegisterAdaptor` | `relay/channel/registry.go` | 个人 Adaptor 工厂 |
| `common.RegisterChannelAPIType` | `common/channel_type_registry.go` | ChannelType → APIType |
| `common.RegisterSkipChatCompletionsToResponses` | `common/chat_responses_skip.go` | 跳过 Chat→Responses 升级 |
| `model.RegisterAfterConsumeLogHook` | `model/hooks.go` | 消费日志写后钩子 |
| `model.RegisterLOGDBModel` | `model/hooks.go` | LOG_DB 个人表注册；`migrateRegisteredLOGDBModels` 统一建表 |

**Echo** 在 `relay/channel/echo/register.go` 的 `init()` 中完成自注册；`relay/relay_adaptor.go` 仅 blank import 该包。

**对话记录** 在 `model/log_conversation.go` 的 `init()` 中注册表 + 钩子；`model/log.go` 只保留一行 `runAfterConsumeLogHooks(...)`。

**LOG 表迁移（避免缺表 + 降低冲突）：**

| 场景 | 行为 |
|------|------|
| 无 `LOG_SQL_DSN`（与主库共用） | `InitLogDB` 只调 `migrateRegisteredLOGDBModels()`，**不**重复迁 `Log{}` |
| 有独立 `LOG_SQL_DSN`（非 CH） | `migrateLOGDB` 先 `AutoMigrate(&Log{})`，再 `migrateRegisteredLOGDBModels()` |
| ClickHouse 日志库 | 仅上游 logs DDL；个人 SQL 表跳过（对话记录不支持 CH） |

---

## 一、对话内容记录功能

### 需求背景

系统记录消耗日志（`logs` 表）时只包含 token 数、模型名、配额等信息，无法查看用户发送的对话内容，不便于 prompt 调试。本功能新增对话内容记录与查看能力，支持选择性开启。

### 后端变更

#### 数据库

**新增表：`conversation_records`**（存储在 `LOG_DB`，经 `RegisterLOGDBModel` 迁移）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | INTEGER (主键, 自增) | 记录 ID |
| `log_id` | INTEGER (索引) | 关联日志 ID |
| `user_id` | INTEGER (索引) | 用户 ID |
| `request_id` | VARCHAR(64) (索引) | 请求 ID |
| `content` | TEXT | 请求体原文（JSON） |
| `created_at` | BIGINT | 创建时间戳 |

#### 新增配置 `ConversationRecordEnabled`

- Key: `ConversationRecordEnabled`，默认 `false`
- 对应变量：`common.ConversationRecordEnabled`
- 涉及：`common/constants.go`、`model/option.go`

#### 记录逻辑

`model/log.go` → `RecordConsumeLog()` 创建日志后调用 `runAfterConsumeLogHooks`；  
`model/log_conversation.go` 在钩子中若开关开启则写入 `conversation_records`。仅消费日志路径触发。

#### 新增 API 接口

| 接口 | 权限 | 说明 |
|------|------|------|
| `GET /api/log/conversation/:log_id` | AdminAuth | 根据日志 ID 查询对话内容 |
| `DELETE /api/log/conversation/?target_timestamp=xxx` | AdminAuth | 删除指定时间之前的对话记录 |

实现文件：`controller/conversation_record.go`（独立文件，避免改 `controller/log.go`）  
路由：`router/api-router.go`（2 行）

### 前端变更（`web/src`，classic 已移除）

#### 日志设置

`web/src/features/system-settings/maintenance/log-settings-section.tsx`：

- 开关：**Enable conversation recording**
- 清除对话记录：日期选择 + 确认删除

类型/默认值：`OperationsSettings.ConversationRecordEnabled`  
（`types.ts` / `operations/index.tsx` / `section-registry.tsx`）

#### 日志详情

- 管理员 + 消费类型日志：详情中 **Conversation → View**
- `web/src/features/usage-logs/components/dialogs/conversation-dialog.tsx`
- `web/src/features/usage-logs/api.ts` — `getConversationByLogId` / `deleteConversationRecords`

> 注意：非 ClickHouse 下管理员列表返回真实 `log.id`；用户侧会改写展示用 ID。对话查询仅管理员接口，且依赖真实 DB id。

---

## 二、回响（Echo）渠道

### 需求背景

为方便测试调试，新增预制渠道类型「回响」，不调用任何 AI 服务，直接返回与输入相同的文字。

### 后端变更

#### 渠道常量（高位 ID，避开上游序列）

`constant/personal.go`：

- `ChannelTypeEcho = 1000`
- `APITypeEcho = 1000`
- `init()` 注册名称 `"Echo"`

**不再**插入 `ChannelTypeDummy` 前，也**不**扩展 `ChannelBaseURLs` 切片。  
`model/channel.go` `GetBaseURL` 对越界 type 做了边界保护。

#### 适配器

`relay/channel/echo/`：

| 文件 | 说明 |
|------|------|
| `constants.go` | 渠道名称 + 模型列表 |
| `adaptor.go` | Echo 适配器实现 |
| `register.go` | `init` 自注册 Adaptor / API 映射 / 跳过 Responses 升级 |
| `adaptor_test.go` | 基础测试 |

**工作流程：**

1. `DoRequest()` 构造本地 `http.Response`（无真实上游）
2. `DoResponse()` 回显最后一条 user 消息
3. 支持流式/非流式 / Responses 模式，token 计为 0

#### 注册（无 switch case）

- blank import：`relay/relay_adaptor.go` → `_ "…/relay/channel/echo"`
- `GetAdaptor` 末尾：`channel.GetRegisteredAdaptor(apiType)`
- `ChannelType2APIType` 末尾：`lookupExtraChannelAPIType`
- Chat→Responses：`service.ShouldChatCompletionsUseResponsesPolicy` 尊重 skip 注册表

### 前端变更

`web/src/features/channels/constants.ts`：

```ts
1000: 'Echo',  // ChannelTypeEcho
```

并加入 `CHANNEL_TYPE_DISPLAY_ORDER`。

---

## 合并上游时预期冲突面

| 区域 | 冲突概率 | 说明 |
|------|----------|------|
| 新文件 `constant/personal.go`、`echo/*`、`hooks.go`、`registry.go`、`middleware/model_redirect.go`、`controller/model_list_personal.go` 等 | 极低 | 仅本分支 |
| `middleware/distributor.go` 薄调用点（`tryModelRedirectSelection`） | 低 | 逻辑已抽到 `model_redirect.go`；上游改 affinity/auto-group 时通常只碰邻行 |
| `controller/model.go` `allowModelInUserList` 一行 | 低 | 计费门禁逻辑在 `model_list_personal.go`；上游改 ListModels 循环结构时重挂一行即可 |
| `controller/relay.go` 重定向重试 / `shouldRetry` | 中 | 与上游 retry/billing 同文件；暂保留内联（函数级），后续可再抽 `relay_personal.go` |
| `model/log.go` 钩子一行 | 低 | 上游若大改 `RecordConsumeLog` 尾部可能需重挂 |
| `model/main.go` `migrateLOGDB` | 低 | 仅包装 `logDBMigrateModels()` |
| `relay/relay_adaptor.go` blank import + registry 回退 | 低 | 尾部 |
| `common/api_type.go` registry 回退 | 低 | 尾部 |
| `service/openai_chat_responses_mode.go` skip 检查 | 低 | 函数开头 |
| `router/api-router.go` 2 条路由 + model_redirect 路由组 | 中 | 路由文件常改，但行少 |
| `common/constants.go` / `model/option.go` 配置项 | 中 | 邻近开关常有新增 |
| 前端 `channels/constants.ts` `1000` 项 | 低 | map 尾部 |
| 前端 log-settings / details-dialog | 中 | UI 重构时需重挂，逻辑在独立 dialog 文件 |

### 2026-08 合并复盘（upstream auto-group 等）

| 冲突文件 | 根因 | 处置 | 防再冲突 |
|----------|------|------|----------|
| `controller/model.go` | 上游重写 ListModels（token limit 改为「组内模型 ∩ limit + FormatMatchingModelName」）；个人在计费门禁加了虚拟模型例外 | 采用上游循环结构 + `allowModelInUserList` | 门禁逻辑迁入 `controller/model_list_personal.go` |
| `middleware/distributor.go` | 上游 affinity 改用 `GetRequestAutoGroups`；个人在 affinity 前插入整段 model redirect | 保留 redirect **优先于** affinity；auto 组统一 `GetRequestAutoGroups` | 整段逻辑迁入 `middleware/model_redirect.go`，主文件仅薄调用 |

**合并时注意**：

1. auto 分组一律走 `service.GetRequestAutoGroups`（支持 token 级 Auto 快照），不要再写死 `GetUserAutoGroup`。
2. 虚拟模型暴露已在 `model/ability.go` `GetGroupEnabledModels` 注入，ListModels 用上游「从组模型过滤」路径即可，不必再回到「只扫 tokenModelLimit key」的旧分支。
3. 若再次在 `distributor.go` / `ListModels` 中部堆业务，优先继续外提个人文件，而不是加长补丁。

**原则**：若某次合并在 switch/枚举尾部再次冲突，优先把改动收回到 `Register*` + 新文件，而不是继续往上游列表中间插。

---

## 完整文件清单

### 对话内容记录

| 文件 | 操作 |
|------|------|
| `common/constants.go` | `ConversationRecordEnabled` |
| `model/option.go` | OptionMap + updateOptionMap |
| `model/hooks.go` | **新** 钩子 / LOGDB 注册 |
| `model/log_conversation.go` | **新/改** 模型 + init 注册 |
| `model/log.go` | `runAfterConsumeLogHooks` 一行 |
| `model/main.go` | `migrateLOGDB` 使用 `logDBMigrateModels()` |
| `controller/conversation_record.go` | **新** API handlers |
| `router/api-router.go` | 2 条路由 |
| `web/src/features/system-settings/...` | 开关 + 清理 UI |
| `web/src/features/usage-logs/...` | 详情查看 + API |

### 回响渠道

| 文件 | 操作 |
|------|------|
| `constant/personal.go` | **新** Echo=1000 |
| `common/channel_type_registry.go` | **新** |
| `common/chat_responses_skip.go` | **新** |
| `relay/channel/registry.go` | **新** |
| `relay/channel/echo/*` | 适配器 + 自注册 |
| `relay/relay_adaptor.go` | blank import + registry |
| `common/api_type.go` | registry 回退 |
| `service/openai_chat_responses_mode.go` | skip 检查 |
| `model/channel.go` | BaseURL 边界保护 |
| `web/src/features/channels/constants.ts` | `1000: Echo` |

### 其它

| 文件 | 操作 |
|------|------|
| `.github/workflows/feat-docker-image.yml` | feat 分支 Docker 构建（若存在） |
| `.github/workflows/canary-docker-image.yml` | Canary 分支多架构镜像发布 |
| `makefile` | `make canary`：feat → Canary 合并推送后回原分支 |
| `docs/features.md` | 本文档 |

---

## 三、模型重定向（高可用虚拟模型）

### 需求背景

普通模型选渠道失败即报错。需要「虚拟模型」按优先级尝试多个「渠道+模型」组合，实现降级高可用；计费按**实际成功**的渠道与模型结算。

### 数据模型

- `model_redirects`：虚拟模型名、可用分组、启用
- `model_redirect_targets`：priority 升序、channel_id、model（空=透传虚拟名）

主库迁移：`RegisterMainDBModel`（`model/hooks.go`）+ `migrateDB`/`migrateDBFast` 末尾调用。

### 运行时

1. `distributor`：解析虚拟模型 → 过滤不可用渠道 / 冷却中 hop → 首档 `SetupContextForSelectedChannel`
2. `relay.getChannel`：重试时按 candidates 下标取下一档；`maxRetry = max(RetryTimes, len-1)`
3. 计费：`OriginModelName` = 当次 attempt 模型；日志 `other.model_redirect` = 客户端虚拟名
4. 钉渠道 / 非虚拟模型：行为不变
5. **失败冷却（进程内）**：某 hop（`channel_id` + hop 开始时的 attempt 模型）在**可降级类**失败后临时禁用  
   - 时长 = `失败次数 × 1 分钟`，上限 **30 分钟**  
   - 仅 hop/上游不可用（401/403/404/429/408/5xx、channel error 等）；skip-retry / 典型客户端 4xx 不冷却  
   - 该 hop **成功**后清零计数；冷却只影响**后续请求**选档  
   - hop key 用 context `original_model`（与 `AttemptModel` 一致），避免 adaptor 改写 `OriginModelName`  
   - 实现：`model/model_redirect_cooldown.go`；选档时 `FilterRedirectCooldownDown`

### Admin

- API：`/api/model_redirect/*`
- 前端：Models 页签 **Model Redirect**（`/models/redirect`）

#### 自定义重定向（嵌套）

- 目标渠道可选哨兵「自定义重定向」`channel_id = -1`，`model` = 其它虚拟模型名。
- 引用语义：运行时展开子规则当前目标链；改子规则无需改父规则。
- 保存时校验自指与环；运行时 depth≤32、展开上限 128。

### 关键文件

| 文件 | 说明 |
|------|------|
| `constant/personal.go` | `ModelRedirectSentinelChannelID = -1` |
| `model/model_redirect.go` | 表/缓存/CRUD/Resolve（含嵌套展开） |
| `model/model_redirect_cooldown.go` | **新** hop 失败临时冷却（1m×N，上限 30m） |
| `controller/model_redirect.go` | Admin API（含嵌套校验） |
| `middleware/model_redirect.go` | **新** 首次选档逻辑（`tryModelRedirectSelection`） |
| `middleware/distributor.go` | 薄调用点（redirect 优先于 affinity） |
| `controller/model_list_personal.go` | **新** ListModels 虚拟模型计费门禁 |
| `controller/model.go` | `allowModelInUserList` 一行 |
| `controller/relay.go` | 降级重试；成功清冷却 / 失败记冷却 |
| `web/src/features/models/*` | UI 页签与表单（含「自定义重定向」） |

---

## 四、消息 Role 兼容（2026-08-04）

### 需求背景

部分上游 OpenAI 兼容接口的 role 枚举较旧，不接受 `developer` 等新角色，会返回 `invalid_request_error`。需要在渠道侧可配置地把不支持角色映射为允许列表中的 fallback。

### 行为

- 渠道 `setting` JSON 字段：
  - `messages_role_compatibility_enabled`（默认 `false`）
  - `messages_role_allowed_list`（开启时必填；默认 `system/assistant/user/tool/function`）
  - `messages_role_fallback`（开启时必填且须在 allowed list 内；默认 `system`）
- 开启后，在发往上游前对 messages 做角色重写；关闭则零行为变化。
- 保存时校验：`ValidateMessagesRoleCompatibility`（`model/channel.go` → `ValidateSettings`）。

### 挂钩点

| 位置 | 说明 |
|------|------|
| `relay/compatible_handler.go` | Chat Completions 转换前 `ApplyMessagesRoleCompatibility` |
| `relay/chat_completions_via_responses.go` | CC→Responses 路径同样应用 |
| `relaykit/dto/channel_settings.go` | 字段 + 校验 + Apply |

### 前端

渠道编辑抽屉「高级设置」：开关 + 允许列表 + fallback；i18n 全语言键。

### 关键文件

| 文件 | 操作 |
|------|------|
| `relaykit/dto/channel_settings.go` | 字段 / Validate / Apply |
| `relaykit/dto/channel_settings_test.go` | 校验与 remap 测试 |
| `model/channel.go` | ValidateSettings 挂校验 |
| `relay/compatible_handler.go` / `chat_completions_via_responses.go` | 调用 Apply |
| `web/src/features/channels/*` | 表单 / 抽屉 / 类型 |
| `web/src/i18n/locales/*` | 文案 |

---

## 五、Sub2API Codex 兼容层（2026-08-05）

### 需求背景

流量经 new-api **Sub2API（type=59）** 打到 sub2api 时，若上游账号开启 `codex_cli_only`，请求必须像官方 Codex 客户端（UA / originator / `x-codex-*`、稳定 session 等）。  
**决策：不新增渠道类型**，在既有 Sub2API 上增加可选兼容层。设计与实现计划见：

- `docs/superpowers/specs/2026-08-05-codex-gateway-channel-design.md`
- `docs/superpowers/plans/2026-08-05-sub2api-codex-compat.md`

### 行为（`codex_compat_enabled=true` 时）

| 能力 | 说明 |
|------|------|
| 身份模式 | `auto` / `passthrough` / `synthesize`（空=auto） |
| Auto | 官方 CLI 身份可过 gate 时透传；否则合成 sticky ID |
| 合成字段 | `User-Agent`（含版本）、`originator`、`session_id` / `thread_id` / `x-codex-window-id` |
| Sticky | 与 body `prompt_cache_key` 对齐；禁止纯随机每请求 ID |
| Compact | `RelayModeResponsesCompact` 时 `Accept: application/json` |
| 关闭时 | 与嵌入的 `newapi.Adaptor` 行为一致 |

渠道 `setting`：`codex_compat_enabled`、`codex_client_version`（auto/synthesize 必填，`X.Y.Z…`）、`codex_client_name`（默认 `codex_cli_rs`）、`codex_identity_mode`。  
校验：`ValidateCodexCompat`。

### 实现结构

| 包/文件 | 职责 |
|---------|------|
| `relay/channel/codexcompat/*` | **新** 纯逻辑：gate 检测、sticky ID、header apply、body `prompt_cache_key` |
| `relay/channel/sub2api/adaptor.go` | 覆盖 `SetupRequestHeader` / `ConvertOpenAIResponsesRequest` |
| `relaykit/dto/channel_settings.go` | 字段 + 校验 |
| 渠道编辑 UI（type=59） | Codex compatibility 开关与子表单项 |

Auth 仍为 `Authorization: Bearer <channel.key>`；路径仍以 `/v1/responses` 等 OpenAI 兼容风格为主。ChatGPT Subscription Codex（57）不变。

---

## 六、Sub2API：Chat Completions → Responses（2026-08-05）

### 需求背景

部分 Sub2API / 上游网关更偏好 `/v1/responses`。全局策略 `global.chat_completions_to_responses_policy` 依赖渠道白名单 + **模型正则**，运营不便。需要**渠道级开关**：只要客户端是 Chat Completions（CC），就转换为 Responses 再打上游。

### 行为

- 渠道 `setting`：`chat_completions_to_responses`（默认 `false`，omitempty）
- 决策顺序（`service.ShouldChatCompletionsUseResponses`）：
  1. 注册 skip 的渠道类型（如 Echo）永不转换  
  2. **渠道强制** `chat_completions_to_responses=true` → 全模型转换  
  3. 否则走全局 policy（allowlist + model patterns）
- 挂接点：`relay/compatible_handler.go`（CC）、`relay/claude_handler.go`（Claude→CC→Responses 既有路径）
- 实际转换复用 `chatCompletionsViaResponses`（临时 `RelayModeResponses` + `RequestURLPath=/v1/responses`）
- Body 透传开启时不转换（与全局一致）
- UI：仅 Sub2API 渠道展示 **Chat Completions → Responses** 开关

### 关键文件

| 文件 | 操作 |
|------|------|
| `relaykit/dto/channel_settings.go` | `ChatCompletionsToResponses` |
| `service/openai_chat_responses_mode.go` | `ShouldChatCompletionsUseResponses` |
| `service/openai_chat_responses_mode_test.go` | **新** 决策测试 |
| `relay/compatible_handler.go` / `claude_handler.go` | 传入渠道强制标志 |
| `web/src/features/channels/*` | 表单 / 抽屉 / 类型 |
| `web/src/i18n/locales/*` | 文案 |

---

## 七、DeepSeek：剥离无签名 thinking 块（2026-08-05）

### 问题

多轮请求把 assistant 的 `thinking` 内容回传时，若缺少上游当初返回的 `signature`，DeepSeek 的 Anthropic 兼容接口 `/anthropic/v1/messages` 会 400。

### 修复

`relay/channel/deepseek/adaptor.go`：`ConvertClaudeRequest` 在后缀 thinking 处理后调用 `stripUnsignedThinkingBlocks`：

- 去掉 `type=thinking` 且 `signature` 为空的 content 块  
- 带有效 signature 的块原样保留  

---

## 八、上游倍率同步：Select Sync Channels 对话框卡死（2026-08-04）

### 问题

系统设置 → 模型倍率上游同步里，「选择同步渠道」对话框打开时可能主线程卡死（Base UI Select 嵌套 Dialog 焦点陷阱 + DataTable 重渲染）。

### 修复

| 点 | 说明 |
|----|------|
| UI | 重写渠道选择：分页普通列表 + native `<select>`，对话框仅 open 时挂载 |
| 数据 | O(n) bulk 解析；大体积 official/models.dev diff 延迟渲染 |
| 文件 | `channel-selector-dialog.tsx`、`upstream-ratio-sync*.tsx`、helpers + 单测 |

---

## 九、Canary 发布流水线（2026-08-03）

| 项 | 说明 |
|----|------|
| Workflow | `.github/workflows/canary-docker-image.yml`：推送 **Canary** 构建 `wryms/new-api:Canary`（amd64+arm64），固定 version `Canary`，并触发 deploy webhook |
| Make | `make canary`：将当前 `feat` 合并进 `Canary` 并 push，完成后切回原分支 |

---

## 近期增量索引（2026-08-03 ~ 2026-08-05）

| 日期 | 主题 | 章节 |
|------|------|------|
| 08-03 | Canary Docker + `make canary` | §九 |
| 08-04 | 消息 role 兼容 | §四 |
| 08-04 | 上游倍率同步渠道选择器卡死 | §八 |
| 08-05 | DeepSeek 无签名 thinking 剥离 | §七 |
| 08-05 | Sub2API Codex 兼容层 | §五 |
| 08-05 | Sub2API CC→Responses 渠道开关 | §六 |

> 更细的 Codex 设计/任务拆分以 `docs/superpowers/` 下 2026-08-05 文档为准；本文只记落地行为与文件面。
