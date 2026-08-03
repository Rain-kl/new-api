# 模型重定向：自定义重定向（嵌套虚拟模型）设计

**日期：** 2026-08-03  
**状态：** 已评审通过（方案 A）  
**范围：** 个人功能「模型重定向」增强 — 目标渠道可选「自定义重定向」，引用其它虚拟模型

---

## 1. 背景与目标

### 现状

模型重定向每个目标是「真实渠道 `channel_id` + 可选上游模型名」。运行时按优先级选出真实渠道并计费。

### 目标

在配置目标时增加特殊渠道 **「自定义重定向」**。选中后，模型侧选择**其它已创建的虚拟重定向模型**（例如 `deepseek-flash`、`gpt-luna`），从而组合成更高层的虚拟模型。

**示例：**

1. 已有虚拟模型 `deepseek-flash`、`gpt-luna`（目标为真实渠道）。
2. 新建虚拟模型 `model`，某一档渠道选「自定义重定向」，模型选 `deepseek-flash` 或 `gpt-luna`。
3. 修改 `deepseek-flash` 的渠道/优先级后，所有引用它的上层模型行为自动与之一致（**引用语义，非保存时拷贝**）。

### 非目标

- 不新增真实渠道行 / 不占用渠道管理列表。
- 不改变真实渠道目标的现有语义与计费规则。
- 不引入加权 LB 实现（Weight 仍为 reserved，与现网一致）。

---

## 2. 决策摘要

| 项 | 决策 |
|----|------|
| 存储方案 | 方案 A：哨兵 `channel_id`，无 schema 迁移 |
| 哨兵值 | `ModelRedirectSentinelChannelID = -1` |
| 引用字段 | `model` = 子虚拟模型名（必填） |
| 同步语义 | 运行时按当前缓存展开；不落快照 |
| 展开语义 | 黑盒嵌套：先完整走子虚拟模型当前整条降级链，再父级下一档 |
| 嵌套深度 | 不限业务深度；工程上限 depth ≤ 32；环检测 |
| UI 可选子模型 | 仅**已启用**且非自身的虚拟模型 |

---

## 3. 数据模型与存储

### 3.1 表结构

**不变。** 继续使用：

- `model_redirects`
- `model_redirect_targets`（`channel_id`、`model`、`priority`、`weight`、`enabled`）

### 3.2 哨兵约定

在个人常量区（建议 `constant/personal.go` 或 `model/model_redirect.go`）定义：

```go
// ModelRedirectSentinelChannelID marks a target as nested virtual-model redirect.
// Not a row in channels; model field holds the child virtual model name.
const ModelRedirectSentinelChannelID = -1
```

| 目标类型 | `channel_id` | `model` | 含义 |
|----------|--------------|---------|------|
| 真实渠道 | `> 0`（存在的渠道 ID） | 可选；空=透传父客户端虚拟名 | 现有行为 |
| 自定义重定向 | `-1` | **必填**，子虚拟模型名 | 引用另一条 `model_redirects.name` |

### 3.3 缓存

`buildModelRedirectCacheMap` 对目标：

- 真实渠道：`ChannelId > 0`（与现网一致）。
- 哨兵：`ChannelId == -1` 且 `Model` 非空 → 写入 `RedirectCandidate{ChannelID: -1, Model: childName, ...}`。

禁止因 `ChannelId <= 0` 一律丢弃（今日实现会丢弃哨兵，**必须改**）。

可选：`RedirectCandidate` 增加只读辅助方法 `IsNestedRedirect() bool`，或调用处判断 `ChannelID == ModelRedirectSentinelChannelID`。不强制改 JSON 形状。

---

## 4. 运行时解析

### 4.1 展开算法（Resolve 路径）

入口保持 `ResolveModelRedirect(clientModel, usingGroup)`，但对返回的候选在进入 `FilterRedirectCandidates` **之前或之内**做**递归展开**，得到仅含**真实渠道**的扁平候选列表（顺序即重试顺序）。

伪代码：

```
expand(name, group, depth, stack) -> []RedirectCandidate (real channels only)
  if depth > 32: return empty (log warn)
  if name in stack: return empty (cycle; log warn)
  push name
  entry = cache[name] if group allowed
  for t in entry.Targets (priority-ordered as today):
    if t is sentinel:
      append expand(t.Model, group, depth+1, stack)   // 整包接在后面
    else:
      append t  // 真实渠道候选
  pop name
  return result
```

**黑盒语义：** 子模型 `deepseek-flash` 的全部（过滤前）目标按子内优先级排在父级该引用档位置；父级下一档在子整包之后。同优先级 LB（`OrderRedirectCandidates`）仍只对**展开后的真实渠道**在过滤可达后执行。

### 4.2 与现有链路衔接

| 步骤 | 行为 |
|------|------|
| `distributor` `tryModelRedirectSelection` | 对顶层虚拟名 `Resolve` → **Expand** → `Filter` → `Order` → 写 `ContextKeyModelRedirectCandidates` |
| `controller/relay.getChannel` 重试 | 仍只消费**真实** `ChannelID` 候选；无需理解哨兵 |
| `FilterRedirectCandidates` | 仅处理真实渠道；哨兵不应出现在展开结果中 |
| 计费 / 日志 | 不变：`OriginModelName` = 当次 attempt 真实模型；`other.model_redirect` = 客户端顶层虚拟名 |

### 4.3 分组

子虚拟模型必须在**当前 `usingGroup`** 下启用（与今日 `ResolveModelRedirect` 的 group 门禁一致）。不可用则该引用档展开为空（等价跳过该档）。

### 4.4 工程护栏

- `modelRedirectMaxExpandDepth = 32`（防环配置漏网 + 栈溢出）。
- 超深或环：该分支丢弃 + `SysLog`/`LogWarn`，不 5xx 整请求（其它档仍可服务）。
- **校验阶段**应尽可能在保存时拒绝环与自指，运行时护栏为兜底。

---

## 5. 校验（创建 / 更新）

`validateModelRedirectInput` 调整：

1. **真实渠道**（`channel_id > 0`）：保持「渠道存在」；`model` 可选。
2. **哨兵**（`channel_id == -1`）：
   - `model` 必填、长度/字符规则同虚拟名。
   - 目标名必须对应**已存在**的 `model_redirects` 记录（建议要求 **enabled=true** 才允许保存引用；与 UI「仅已启用」一致）。
   - 不得等于当前正在编辑的虚拟名（自指）。
3. **环检测（保存时）：**  
   在「当前输入 + 库中其它规则」组成的有向图上，从当前名 DFS/拓扑检测；若加入本条 targets 的引用边后存在环 → 拒绝。  
   更新时用本次提交的 targets 替换该节点出边再检测。
4. `channel_id` 既不是 `>0` 也不是 `-1` → 非法。
5. 至少一条 **enabled** 目标；目标数上限不变（64）。

删除子虚拟模型时：若仍被其它规则引用，**允许删除**（运行时展开为空跳过），或可选返回 409。  
**本设计选择：允许删除**；上层引用变成「空档」直到管理员修复。文档与 UI 可在子模型删除后列表仍显示原 `model` 字符串（渠道显示「自定义重定向」）。

---

## 6. 前端 UI

文件主战场：`web/src/features/models/components/model-redirect-section.tsx`。

### 6.1 渠道选择

- 在渠道选项列表**固定插入**一项：  
  `{ id: -1, name: t('自定义重定向') }`（或英文 key `Custom redirect`，i18n 按项目规范）。
- 不占用真实渠道 API 数据；前端合成即可。

### 6.2 模型选择

- `channel_id > 0`：保持现有「该渠道 models 列表 / 可输入」（现有 MultiSelect 行为）。
- `channel_id === -1`：选项 = 当前全部**已启用**虚拟模型名（来自 `listModelRedirects`），**排除**正在编辑的自身名称；**单选**（一档一个子模型）。清空/切换渠道时重置 model。

### 6.3 提交映射

- 哨兵行：`channel_id: -1`，`model: 子虚拟名`（必填校验）。
- `draftsToTargets`：允许 `channel_id === -1`（今日 `channel_id <= 0` 会 skip，**必须改**）。

### 6.4 列表展示

- 哨兵：`自定义重定向 → {model}`（mono）。
- 真实渠道：保持 `渠道名 / 模型`。

### 6.5 i18n

新增键（English source keys）：

- `Custom redirect`
- 如需：`Select a redirect model`、校验文案等。

---

## 7. 测试

### 后端（`model/model_redirect_test.go` 等）

| 用例 | 期望 |
|------|------|
| 叶子虚拟模型展开 | 与现网一致，真实渠道列表 |
| A 引用 B（真实渠道链） | 顺序 = B 的全部目标 + A 的后续真实目标 |
| B 变更优先级后重新 Load 缓存 | A 展开顺序跟随 B（引用语义） |
| 自指 / A→B→A | 校验错误 |
| 运行时环（校验被绕过） | 展开截断，无 panic |
| depth > 32 | 截断 + 无 panic |
| 哨兵 model 为空 | 校验错误 |
| 子模型 group 不含 usingGroup | 该档展开为空 |

### 前端

- 选「自定义重定向」后模型选项为虚拟名；不含自身。
- 提交 payload 含 `channel_id: -1`。

---

## 8. 文件影响面（实现指引）

| 区域 | 文件 | 变更 |
|------|------|------|
| 常量 | `constant/personal.go` 或 model 包内 | 哨兵常量 |
| 核心 | `model/model_redirect.go` | 缓存收录哨兵；Expand；校验环；`Filter` 不碰哨兵 |
| 测试 | `model/model_redirect_test.go` | 上表用例 |
| 分发 | `middleware/model_redirect.go` | Resolve 后 Expand（若 Expand 内聚在 Resolve 则可不动） |
| 文档 | `docs/个性化需求.md` | 补充「自定义重定向」 |
| UI | `model-redirect-section.tsx` + i18n | 渠道项、模型源、展示、校验 |

**低冲突原则：** 展开/环检测优先放在 `model/model_redirect.go`（或同目录新文件 `model_redirect_expand.go`），避免加厚 `distributor.go`。

---

## 9. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 深嵌套放大候选数 | 目标总数上限 64/规则；展开后可再 cap（建议展开后 max 128 candidates，超出截断并日志） |
| 删除被引用子模型 | 允许；运行时空档；列表仍显示引用名便于修复 |
| 与 affinity 交互 | 顶层仍先 model-redirect 再 affinity（不变） |
| 负数 channel_id 被前端/JSON 弄丢 | 前后端单测锁住 `-1` |

---

## 10. 验收标准

1. 可创建 `deepseek-flash`、`gpt-luna`（真实渠道目标）。
2. 可创建 `model`，目标含「自定义重定向 → deepseek-flash」。
3. 请求 `model` 时，实际尝试顺序与当时直接请求 `deepseek-flash` 的渠道顺序一致（在同一 group 下），其后才是 `model` 的其它档。
4. 修改 `deepseek-flash` 优先级并保存后，**无需**改 `model`，下次请求顺序已变。
5. 配置环被拒绝；UI 不提供自身作为自定义重定向目标。

---

## 11. 已否决方案

- **方案 B（target_type 字段）：** 语义更清晰但需迁移，本期不做。
- **方案 C（保存时扁平拷贝）：** 与「和原模型统一」冲突。
