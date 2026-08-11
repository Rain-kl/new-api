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
| `common/utils.go` `NormalizeRelaySelectionPath` | 低 | 尾部追加新函数；`distributor.go` 选渠道入口引用一行 |
| 前端 `channels/constants.ts` `1000` 项 | 低 | map 尾部 |
| 前端 `channel-favicon.tsx` | 低 | 新文件；`channels-columns.tsx` 导入一行 |
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

## 五、Sub2API Codex 兼容层（2026-08-05）【已移除：能力迁移至 §十】

> **2026-08-07 已移除**：该兼容层及其 `codex_identity_mode` / body 塑形 / ID 合成逻辑已删除
> （`relay/channel/codexcompat/*` 包移除，commit `1bd7c844`）。「模拟 Codex 客户端」能力现由
> **Advanced Custom（type=58）** 渠道的 `codex_compat_enabled` 提供（见 §十）：仅设置官方
> Codex CLI 的 `User-Agent` / `originator` 并透传客户端 `session_id` / `thread_id` / `x-codex-*`
> 请求头，**不做**任何 ID 合成。遗留 Sub2API 渠道上的 codex 设置不再生效，
> `ValidateCodexCompat` 仅对 Advanced Custom 渠道执行。以下为历史行为记录。

### 需求背景（历史）

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

## 六、Sub2API：Chat Completions → Responses（2026-08-05）【已移除】

> **2026-08-07 已移除**：渠道级 `chat_completions_to_responses` 开关已删除（commit `e6f3a268`）。
> CC→Responses 的决策回归全局策略 `global.chat_completions_to_responses_policy`
> （allowlist + 模型正则）。Advanced Custom 渠道如需 Chat Completions → Responses，
> 请使用路由级 converter `openai_chat_completions_to_openai_responses`。以下为历史行为记录。

### 需求背景（历史）

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

## 十、Advanced Custom：模拟 Codex / Claude Code 客户端（2026-08-07）

### 需求背景

部分上游网关按**客户端身份**放行：有的只认官方 Codex CLI（UA / originator / `x-codex-*`），
有的只认 Claude Code CLI（claude-cli UA + `anthropic-beta: claude-code-20250219` + `x-app: cli`）。
在 **Advanced Custom（type=58）** 渠道上提供两个开关，让出站请求伪装成对应官方 CLI。

### 行为

| 开关 | 字段 | 行为 |
|------|------|------|
| Simulate Codex client | `codex_compat_enabled` | 设置官方 Codex CLI `User-Agent`（`{codex_client_name}/{codex_client_version} (linux; x86_64)`）与 `originator`；透传客户端 `session_id` / `thread_id` / `x-codex-*` 请求头（名字按 http.Header 规范化）；**不做**任何 ID 合成 |
| | `codex_client_version` | 必填（`X.Y.Z[+suffix]`），用于 UA 版本段 |
| | `codex_client_name` | 可选，默认 `codex_cli_rs`，用于 UA 客户端段与 `originator` |
| Simulate Claude Code client | `claude_compat_enabled` | claude-cli UA（`claude-cli/1.0.119 (external, cli)`）、`anthropic-beta: claude-code-20250219`、`x-app: cli`；并把 Claude Code 身份行（`You are Claude Code, Anthropic's official CLI for Claude.`）作为**第一个 system 块**幂等注入（已存在则不动） |
| | 适用范围 | 仅对 Claude Messages 目标路由生效：converter `openai_chat_completions_to_anthropic_messages` / `openai_responses_to_claude_messages`，或 native `/v1/messages`（converter `none` + `RelayFormat=claude`） |

> **注意**：两个开关面向不同上游（Codex 门控 vs Claude Code 门控），同一路由上不应同时开启。

### 实现结构

| 文件 | 职责 |
|------|------|
| `relay/channel/advancedcustom/compat.go` | header 注入 + Claude Code 身份行注入 |
| `relay/channel/advancedcustom/adaptor.go` | `SetupRequestHeader` / Convert 路径挂钩 |
| `relaykit/dto/channel_settings.go` | 字段 + `ValidateCodexCompat`（仅 Advanced Custom 渠道校验） |
| `model/channel.go` | `ValidateSettings` 按渠道类型门控 |
| `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | 开关 UI |
| `web/src/i18n/locales/*` | 文案 |

---

## 十一、模型重定向：模型映射模式（2026-08-11）

### 需求背景

优先级重定向（§三）的目标是「渠道 + 模型」，渠道调整后需要逐个重配。新增**模型映射**模式：虚拟模型映射到目标模型名，由**渠道层按模型自动选渠道**，渠道增减无需改配置。两种模式按虚拟模型独立选择，且**切换模式不丢失另一模式的配置**（非破坏性）。

### 数据模型

`model_redirects` 新增两列（AutoMigrate，`RegisterMainDBModel`）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `mode` | varchar(16) | `redirect`（默认）/ `mapping` |
| `mapping_target` | varchar(128) | 映射模式的目标模型名 |

- 非破坏性更新：mode=mapping 时只写 `mapping_target`，渠道链 targets 原样保留；mode=redirect 时反向同样。
- 校验：映射目标必填、≤128 字符、无逗号/控制空白；若命中已有虚拟模型须为启用；**映射边参与环检测**（`a→b→a` 与自引用在保存时拒绝）。

### 运行时：递归解析 + 纯模型候选

- 映射条目解析为**纯模型候选**（`RedirectCandidate.ChannelID==0`），渠道由渠道层按模型选取。
- 映射目标不是终止节点：重新查虚拟模型列表继续解析，直至结果不在列表（普通渠道路由）或命中重定向链：
  - `映射→映射`：继续映射（a→b→c 链）
  - `映射→重定向`：走重定向优先级链
  - `重定向→映射`：嵌套引用映射条目 → 纯模型候选（继承 sentinel 优先级）
- 重试槽位 walk（`model.ResolveRedirectSlot`）：渠道绑定候选占 1 槽；纯模型候选占 `RetryTimes+1` 槽（复用 `GetRandomSatisfiedChannel` 的渠道优先级档），档位用尽再降级下一候选；`maxRetry = max(RetryTimes, len-1 + 纯模型数×RetryTimes)`。
- 计费 / 广场 / 日志：`ModelRedirectDisplaySourceModel` 对映射条目返回 `mapping_target`；plaza / ListModels 自动可见；日志 `model_redirect_attempt` = 映射目标。
- 上下文新增 `ContextKeyModelRedirectGroup`（`constant/personal.go`）：重试槽位 walk 选渠道所需的有效分组。

### Admin / 前端

- `/models/redirect` 每条可切「模型映射 / 模型重定向」模式；映射模式只填「映射到（目标模型名）」。
- 表格新增「模式」徽标列；映射行目标列显示 `→ 映射目标`。

### 同日修复

| 问题 | 根因 | 修复 |
|------|------|------|
| 优先级目标「启用」开关关闭不生效 | `Enabled bool gorm:"default:true"` 让 GORM 在 Create 时跳过 `false` 零值，落库为启用 | 移除 `ModelRedirectTarget.Enabled` 的 default 标签（默认值由 `buildTargetsFromInput` 代码强制） |
| 新建「禁用」条目不生效 | 同上，`ModelRedirect.Enabled` | 移除父表 `Enabled` 的 default 标签 |
| 新增目标默认优先级 | 旧逻辑 `max + 10`（新目标变最高优先） | 改为 `min - 10`（新目标作为更低降级档），下限 1 |

### 关键文件

| 文件 | 说明 |
|------|------|
| `model/model_redirect.go` | `mode`/`mapping_target` 字段、递归解析、纯模型候选、槽位 walk、校验/CRUD、显示辅助 |
| `model/model_redirect_cooldown.go` | 过滤器保留纯模型候选 |
| `middleware/model_redirect.go` | 首选取（含纯模型选渠道）+ 裁剪重试列表 |
| `controller/relay.go` | 槽位 walk 重试 + maxRetry 公式 |
| `constant/personal.go` | `ContextKeyModelRedirectGroup` |
| `web/src/features/models/*` | 模式切换抽屉 + 徽标列 |

---

## 十二、Playground 路径归一化：高级自定义渠道选渠道（2026-08-11）

### 问题

`/pg/chat/completions` 请求在 **Distribute 选渠道阶段**使用原始路径，而 Advanced Custom（type=58）渠道按**精确路径**匹配路由（如 `/v1/chat/completions`），导致 playground 请求在所有高级自定义渠道上「无可用渠道」。`GenRelayInfo` 只在 relay 阶段归一化上游路径，选渠道阶段此前未归一化。

### 修复

| 点 | 说明 |
|----|------|
| `common.NormalizeRelaySelectionPath` | `/pg/...` → `/v1/...`（非 playground 原样返回） |
| `middleware/distributor.go` | 选渠道入口计算 `selectionPath`，传入重定向 / 亲和 / `CacheGetRandomSatisfiedChannel`；**不改 `c.Request.URL.Path`**（`IsPlayground` 计费标志依赖原始路径） |
| `controller/relay.go` | 重试参数与 `ResolveRedirectSlot` 同样归一化 |

### 关键文件

`common/utils.go`、`common/utils_test.go`、`middleware/distributor.go`、`controller/relay.go`。

---

## 十三、渠道名称列上游 favicon（2026-08-11）

### 行为

渠道名称前显示上游网站图标（`https://favicon.im`），便于在渠道很多时快速识别归属。

- 从渠道 `base_url` 提取域名 → `https://a.favicon.im/{domain}`
- `loading="lazy"`，`onError` 自动隐藏；跳过 localhost / IP 字面量
- 敏感掩码开启（名称显示 `••••`）时不显示图标
- 表格与卡片布局均生效（卡片复用名称列渲染器）

### 关键文件

`web/src/features/channels/components/channel-favicon.tsx`（**新**）、`web/src/features/channels/components/channels-columns.tsx`。

---

## 十四、渠道模型映射：未定价模型按映射目标计费（2026-08-11）

### 需求背景

渠道配置了 `model_mapping`（如 `"gpt-3.5" → "gpt-3.5-turbo"`）时，预扣费计费此前只按**客户端请求模型**（`OriginModelName`）查费用。若该模型未配置任何费用（无价格、无倍率、无 tiered 表达式）且自用模式关闭、用户未开启「接受未定价模型」，请求直接报「模型 X 的价格未配置」——即使映射目标模型已配置费用。现改为**按映射目标的费用计费**，映射渠道不再需要给每个客户端模型单独定价。

### 行为

| 场景 | 计费基准 |
|------|----------|
| 请求模型已配置费用（价格 / 倍率 / tiered_expr） | 请求模型（不变） |
| 用户开启「接受未定价模型」 | 请求模型，按 37.5 默认倍率（不变） |
| 请求模型未配置费用 + 渠道 `model_mapping` 链尾目标已配置费用 | **映射目标模型**（本次新增） |
| 无映射 / 映射目标也未定价 / 映射链成环 | 仍报「价格未配置」（不变） |

- 映射链解析带**循环检测**（`a→b→a` 不成环时正常返回链尾；成环不回退）；自映射视为未映射。
- 应用于 token 计费 `ModelPriceHelper` 与按次计费 `ModelPriceHelperPerCall`（MJ / Task）。
- 映射目标为 tiered_expr 时按目标表达式计费（`modelPriceHelperTiered` 增加 `billingModel` 参数）。
- Responses Compact 模型先剥 `-openai-compact` 后缀再走映射链，与 `ModelMappedHelper` 的上游模型推导一致。
- 客户端模型 `OriginModelName` **不变**（消费日志仍记录请求模型）；`PriceData` 写入映射目标的费用，预扣费与结算口径一致。

### 实现

- 数据来源：`Distribute()` 中间件经 `SetupContextForSelectedChannel` 写入 gin context 的 `model_mapping`（计费执行时渠道已选定）。
- `relay/helper/price.go` 新增 `resolveBillingModel` / `channelMappedModel`；`ModelPriceHelper` / `ModelPriceHelperPerCall` 所有费用查询改用解析出的计费模型。

### 关键文件

| 文件 | 说明 |
|------|------|
| `relay/helper/price.go` | `resolveBillingModel` / `channelMappedModel` + 两处 PriceHelper 改用计费模型 |
| `relay/helper/price_test.go` | 映射回退 / 无映射报错 / 成环不回退 / 已定价不受影响 / 接受未定价保持 37.5 / compact 用例 |

---

## 近期增量索引（2026-08-03 ~ 2026-08-11）

| 日期 | 主题 | 章节 |
|------|------|------|
| 08-03 | Canary Docker + `make canary` | §九 |
| 08-04 | 消息 role 兼容 | §四 |
| 08-04 | 上游倍率同步渠道选择器卡死 | §八 |
| 08-05 | DeepSeek 无签名 thinking 剥离 | §七 |
| 08-05 | Sub2API Codex 兼容层（已移除） | §五 |
| 08-05 | Sub2API CC→Responses 渠道开关（已移除） | §六 |
| 08-07 | Advanced Custom 模拟 Codex / Claude Code 客户端 | §十 |
| 08-11 | 模型重定向：模型映射模式 + 同日修复 | §十一 |
| 08-11 | Playground 路径归一化（高级自定义选渠道） | §十二 |
| 08-11 | 渠道名称列上游 favicon | §十三 |
| 08-11 | 渠道模型映射：未定价模型按映射目标计费 | §十四 |

> 更细的 Codex 设计/任务拆分以 `docs/superpowers/` 下 2026-08-05 / 2026-08-06 文档为准；本文只记落地行为与文件面。
