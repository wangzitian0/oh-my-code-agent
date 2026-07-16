# Harness Profile Resolver — Project Initialization

## 项目目标

构建一个 local-first、vendor-neutral 的 Coding TUI harness 治理层，首批兼容 Claude Code、OpenAI Codex 与 OpenCode。

项目只解决三个核心问题：

1. **Ontology**：定期对各 Coding TUI 暴露的配置概念求并集，并维护等价、部分等价和厂商特有概念之间的映射。
2. **Drift**：识别同一逻辑实体在多个 TUI、多个 scope 和多个物理位置上的值差异、缺失、覆盖与异常加载。
3. **Profiles**：将个人、公司、团队、项目，以及后续可选的任务 Profile，在启动时动态组合成一次会话的 Effective Harness。

核心解析链：

```text
Context → Select Profiles → Compose Entities → Apply Policies
        → Materialize Runtime View → Launch TUI → Verify Effective State
```

Harness 是运行时计算结果，不是永久安装在全局目录中的静态配置。

## 问题背景

当前 Coding TUI 的配置分散在：

- 用户全局目录；
- 共享 Agent 目录；
- 仓库目录；
- 插件目录；
- 安装器维护目录；
- 账号级 Connector；
- 公司内部工具目录。

不同安装器普遍以“保证到处可用”为目标，把 Skills、MCP、Hooks、Instructions、Connectors 和 Plugins 安装到全局。这会导致：

- 公司能力进入个人项目；
- 项目专用工具全局加载；
- 同一 Skill 在多个位置存在不同版本；
- 高优先级配置覆盖项目配置；
- 已卸载工具留下孤立 Instruction；
- 不知道某个配置由谁安装、在哪里生效；
- Hooks、Loops、Agents、Plugins 等语法糖增加后，供应商锁定不断加深；
- 无法回答“当前仓库实际运行的 Harness 是什么”。

## 产品原则

1. Runtime composition 优先于 global installation。
2. 以 logical entity 为中心，不以文件为中心。
3. Ontology 取各 TUI 能力并集，不退化为最小公分母。
4. Profiles 是可组合集合，不是硬编码的四层继承树。
5. 每种 ontology concept 拥有自己的合并策略。
6. 默认只读、可解释、可复现。
7. 发现、解析、优先级和 Drift 判定尽量确定性完成。
8. 大模型负责复杂语义差异的解释和修复建议。
9. 本地优先，配置与 Secret 默认不上传。
10. 厂商特有能力以 Vendor Extension 保存，不能静默丢失。
11. 所有写操作必须支持 dry-run、备份和回滚。
12. Generated Harness 是临时构建产物，不能反向成为隐式真源。

## Profile 模型

初始支持：

```text
personal:<identity>
company:<organization>
team:<team>
project:<repository>
task:<temporary-purpose>   # Post-MVP
```

一次运行上下文示例：

```yaml
context:
  personal: wangzitian
  company: sea
  team: marketplace-buyer
  project: order-service
  task: incident-debug
```

这些维度不是固定的优先级链。例如：

- 公司 deny policy 不允许被团队、项目或个人覆盖；
- 项目 Instruction 可以覆盖个人编码偏好；
- 个人 UI 偏好不应被团队控制；
- 项目可以增加自己的 Skill；
- 公司禁用的 MCP 不能被项目重新启用。

## Ontology

### MVP Concepts

```text
instruction
skill
mcp_server
```

### Post-MVP Concepts

```text
command
agent
hook
permission
plugin
connector
memory
loop
background_task
session_policy
```

每个 concept 必须定义：

- stable concept ID；
- logical entity identity 规则；
- 各 TUI 的表示方式；
- global/project 等发现位置；
- TUI 内部 precedence；
- normalization 规则；
- merge strategy；
- portability classification；
- known lossy mappings；
- vendor extensions；
- ontology schema version。

示例：

```yaml
concept: skill

identity:
  primary: metadata.id
  fallback:
    - source_repository_and_name
    - directory_name
    - normalized_title

merge: union_by_logical_id

representations:
  claude:
    global:
      - ~/.claude/skills/*
    project:
      - .claude/skills/*
  codex:
    global:
      - ~/.codex/skills/*
      - ~/.agents/skills/*
    project:
      - .codex/skills/*
      - .agents/skills/*
  opencode:
    global:
      - ~/.config/opencode/skills/*
    project:
      - .opencode/skills/*
      - .agents/skills/*
      - .claude/skills/*
```

### Ontology 更新

Ontology 与用户 Profile 分开版本化。更新流程：

1. 检查官方文档、Schema、CLI help 和已知配置位置；
2. 发现新增 concept、field、location 或 precedence；
3. 与现有 ontology 做匹配；
4. 分类为 EXACT、EQUIVALENT、PARTIAL、VENDOR_ONLY 或 UNKNOWN；
5. 人工确认后固化为确定性映射；
6. 为每个支持的 TUI 版本保存 regression fixtures。

LLM 可以提出映射候选，但运行时不能每次重新猜测已确认映射。

## Logical Entity

Scanner 必须区分逻辑实体和物理表示：

```yaml
entity:
  concept: skill
  logical_id: deep-research

locations:
  - host: claude
    scope: personal
    path: ~/.claude/skills/deep-research/SKILL.md
  - host: codex
    scope: personal
    path: ~/.agents/skills/deep-research/SKILL.md
  - host: opencode
    scope: project
    path: .opencode/skills/deep-research/SKILL.md
```

匹配优先级：

1. 显式 stable logical ID；
2. source repository + entity name；
3. MCP server name 等原生稳定 ID；
4. 已知路径映射；
5. normalized structure；
6. 文本相似度或 LLM 建议，需人工确认。

模糊匹配一旦确认，应保存映射，避免反复推断。

## Drift Model

必须比较两类 Drift：

1. **Source Drift**：同一逻辑实体在多个物理位置的值不同。
2. **Effective Drift**：实际 TUI 加载结果与 Profile 解析出的期望状态不同。

状态分类：

```text
IDENTICAL   原始值或 Canonical Value 完全一致
EQUIVALENT  格式不同，但归一化语义等价
DRIFTED     对应值存在实质差异
MISSING     期望位置或能力缺失
UNEXPECTED  实际加载了 Profile 未选择的能力
SHADOWED    一个位置被更高优先级位置覆盖
ORPHANED    实体引用的依赖、安装器或来源已经消失
UNKNOWN     无法安全确认身份或等价性
```

每条 Drift 至少输出：

- concept 和 logical entity ID；
- 所有发现位置；
- raw hash 与 normalized hash；
- Profile 期望状态；
- precedence 与 shadowing 原因；
- provenance；
- 潜在行为影响；
- repairability；
- 供 LLM 判断的相关内容或 diff；
- 推断置信度与证据。

Scanner 本身不得依赖远端 LLM。

## Profile 格式

个人 Profile：

```yaml
apiVersion: harnessctl.dev/v1alpha1
kind: Profile

metadata:
  id: personal:wangzitian

include:
  skills:
    - deep-research
  mcp:
    - github

preferences:
  language: zh-CN
```

公司 Profile：

```yaml
apiVersion: harnessctl.dev/v1alpha1
kind: Profile

metadata:
  id: company:sea

include:
  mcp:
    - skynet
    - jira

policy:
  mcp:
    deny:
      - gmail
  network:
    allow:
      - "*.sea.com"
```

项目声明：

```yaml
apiVersion: harnessctl.dev/v1alpha1
kind: ProjectHarness

profiles:
  - personal:wangzitian
  - company:sea
  - team:marketplace-buyer

include:
  skills:
    - order-state-machine
  mcp:
    - codegraph

exclude:
  skills:
    - unrelated-finance-workflow
```

Profile 支持：

- include；
- exclude；
- parameters；
- match conditions；
- policy；
- Secret reference。

Secret 只能保存引用，不能写入 Profile 内容或 Diff。

## Composition Semantics

不能使用统一的 Last Write Wins。

```yaml
merge_strategies:
  skills: union_by_logical_id
  mcp_servers: merge_by_logical_id
  instructions: ordered_compose
  permissions: deny_wins
  preferences: nearest_scope_wins
  vendor_extensions: preserve_per_host
```

冲突必须保留：

- 双方值；
- Provenance；
- 冲突原因；
- 未能自动解析的规则。

Effective Harness 必须可以解释：

```text
payment-debug came from team:marketplace-buyer
codegraph came from project:order-service
gmail was excluded by company:sea policy
language=zh-CN came from personal:wangzitian
```

## Context Detection

支持自动检测和显式覆盖。

自动信号包括：

- 当前 Git root；
- Git remote；
- 仓库中的 `.harness.yaml`；
- 目录祖先；
- repository/company/team mapping；
- 环境 marker；
- CLI 显式增删 Profile。

示例：

```bash
harnessctl run codex +task:incident-debug -personal:finance
```

启动前必须显示检测结果和 Profile 来源，并允许覆盖。

## Runtime Effective Harness

每次运行生成不可变快照：

```text
~/.cache/harnessctl/runs/<run-id>/
├── effective.yaml
├── provenance.json
├── drift.json
├── claude/
├── codex/
└── opencode/
```

快照包含：

- detected context；
- selected profiles；
- resolved entities；
- blocked/degraded entities；
- provenance；
- ontology version；
- source hashes；
- adapter versions；
- generation time；
- reproducible resolution fingerprint。

生成的 Host 配置是一次性 build output。

## 动态加载策略

按优先顺序使用侵入性最低的方式：

1. TUI 官方支持的 config/home 参数；
2. project-local generated runtime directory；
3. atomic symlink/runtime view；
4. 能保证恢复的 temporary overlay。

正常启动路径禁止覆盖真实全局配置。

Skills 通过 per-run 链接目录加载。Instructions 通过 per-run 生成视图加载。

MCP 后续优先设计为单一 local gateway：

```text
All TUIs → harnessctl MCP Gateway → Context-filtered MCP servers/tools
```

Gateway 根据 Effective Harness 动态暴露工具，避免所有 MCP 被永久注册为全局能力。

## LLM-Assisted Repair Boundary

程序确定性负责：

- discovery；
- parsing；
- logical identity candidates；
- precedence；
- normalization；
- profile expectation；
- drift；
- provenance；
- backup 与 atomic write。

当前 Coding TUI 中的大模型负责：

- 解释自然语言差异；
- 判断差异互补还是冲突；
- 提出 merged value；
- 修改 canonical Profile 或 Asset；
- 重新生成 Runtime View；
- 验证修复后的状态。

后续提供 CLI JSON 与本地 MCP：

```text
harness.status
harness.diff
harness.explain
harness.plan_repair
harness.apply
harness.validate
```

权限扩大、Secret 暴露或破坏性写入必须显式确认。

## MVP

### Supported TUIs

- Claude Code
- OpenAI Codex
- OpenCode

### Supported Concepts

- Instructions
- Skills
- MCP Servers

### Supported Profiles

- Personal
- Company
- Team
- Project

### Required Commands

```bash
harnessctl scan
harnessctl profiles resolve
harnessctl diff
harnessctl explain
harnessctl run <claude|codex|opencode>
```

### Required Behavior

1. 发现三个 TUI 的 global/project 配置；
2. 解析为统一 Inventory；
3. 将明显对应的表示匹配为 Logical Entity；
4. 根据 Context 和项目声明选择 Profiles；
5. 生成带 Provenance 的 Effective Harness；
6. 报告 MISSING、UNEXPECTED、DRIFTED、SHADOWED、ORPHANED 和 UNKNOWN；
7. 在隔离 Runtime View 中加载 Skills 与 Instructions；
8. 不永久修改全局配置地生成或暴露 MCP 配置；
9. 第一阶段至少启动一个 TUI，再扩展到另外两个；
10. 所有只读命令提供 JSON 输出。

## MVP Non-goals

- GUI；
- Hosted SaaS；
- 企业用户管理；
- 自动语义冲突解决；
- 保证三个 TUI 体验完全一致；
- Memory/Session 迁移；
- Plugin Marketplace；
- Secret Manager；
- 后台常驻 Daemon；
- 支持所有 Coding Assistant；
- 未审核的自动 Ontology 更新；
- 替代 CC Switch、SkillDock 或现有 Package Manager。

## 建议仓库结构

```text
.
├── cmd/harnessctl/
├── internal/
│   ├── ontology/
│   ├── discovery/
│   ├── identity/
│   ├── normalize/
│   ├── profiles/
│   ├── resolver/
│   ├── drift/
│   ├── runtime/
│   └── adapters/
│       ├── claude/
│       ├── codex/
│       └── opencode/
├── ontology/
│   ├── concepts/
│   ├── hosts/
│   └── versions/
├── profiles/examples/
├── fixtures/
│   ├── claude/
│   ├── codex/
│   └── opencode/
├── schemas/
├── docs/
└── tests/
```

优先单一跨平台二进制。Go 是默认选择：项目主要涉及文件发现、解析、Profile Resolution、CLI、进程启动和后续文件监听。若实现者明确偏好 Rust，也可以选择 Rust。不要为了假设的性能优势提前复杂化。

## 核心接口草案

```go
type HostAdapter interface {
    Detect(ctx Context) ([]Installation, error)
    Scan(ctx Context) ([]Representation, error)
    Normalize(rep Representation) (CanonicalValue, error)
    Materialize(effective EffectiveHarness, targetDir string) error
    Launch(runtime RuntimeSnapshot, args []string) error
}

type Resolver interface {
    Resolve(ctx Context, profiles []Profile, inventory Inventory) (EffectiveHarness, error)
}

type DriftEngine interface {
    Compare(inventory Inventory, expected EffectiveHarness) ([]Drift, error)
}
```

Ontology 尽量声明式维护；Host-specific parsing 和 materialization 放在 Adapter。

## 安全与正确性

- 只读命令绝不修改源文件；
- Generated directory 必须标记并加入 Git ignore；
- 不在日志、Diff、Provenance 或 JSON 中输出 Secret；
- Secret reference 与普通配置分开；
- 拒绝未知 Ontology Schema Version；
- 保留未知 Vendor Field；
- Generated output 使用 atomic write；
- 修改非生成文件前必须备份；
- Applied change 写入本地 Ledger；
- 检测 symlink，不能静默遍历意外目标；
- 只扫描已知配置根目录；
- 权限扩大必须明确确认；
- 所有写操作支持 dry-run。

## 测试策略

1. 每个 TUI 和版本的 Golden Fixtures；
2. Parser/Normalizer Unit Tests；
3. Logical Identity Matching Tests；
4. Profile Composition 与 Policy Conflict Tests；
5. Drift Classification Tests；
6. 声称 Lossless 的 Adapter 必须有 Round-trip Tests；
7. Runtime Materialization Snapshot Tests；
8. 使用临时 HOME/Config 的 Integration Tests；
9. Secret Redaction Tests；
10. 证明只读命令不会修改 Fixture 的测试。

必须保持的 Invariants：

```text
相同输入必须产生相同 fingerprint
scan 不写入
unknown fields 在支持的 round trip 中不丢失
deny policy 不能被低层 profile 削弱
每个 effective value 必须有 provenance
generated artifact 不会隐式成为 canonical source
```

## Milestones

### M0 — Evidence and Schemas

- 收集脱敏后的 Claude、Codex、OpenCode 配置 Fixture；
- 记录发现位置和 Precedence；
- 定义 Ontology、Profile、ProjectHarness、EffectiveHarness v1alpha1 Schema；
- 写 Runtime Isolation ADR。

### M1 — Inventory and Doctor

- 实现 Instructions、Skills、MCP 的 scan；
- 生成 normalized inventory；
- 识别重复、缺失、意外、覆盖和明显 Drift；
- 输出人类可读格式和 JSON。

### M2 — Profile Resolver

- 加载 Personal/Company/Team/Project Profiles；
- 检测当前 Context；
- 使用 Concept-specific Merge Rules 组合；
- 生成 effective.yaml 和 provenance.json。

### M3 — Runtime Launch

- 为一个 TUI 生成隔离 Runtime View；
- 在不永久修改全局配置的情况下启动；
- 再增加另外两个 Adapter。

### M4 — Agent-Assisted Repair

- 输出结构化 Repair Context；
- 增加 Portable Skill 或 Local MCP；
- 写操作要求 Preview 和确认。

### M5 — Ontology Update Workflow

- 版本化 Host Capability Data；
- 检测上游文档或 Schema 的候选变化；
- 通过人工审核和 Regression Fixtures 发布更新。

## MVP 验收标准

在包含 `.harness.yaml` 的仓库中执行：

```bash
harnessctl scan
harnessctl profiles resolve
harnessctl diff
harnessctl run codex
```

必须得到：

1. Global 与 Project 配置 Inventory；
2. 被选择的 Personal/Company/Team/Project Profiles；
3. 仅包含期望 Assets 的 Effective Harness；
4. 每个 Included/Blocked Entity 的 Provenance；
5. Drift、Missing、Unexpected 和 Shadowing 报告；
6. 使用隔离 Runtime View 启动的 Coding TUI；
7. 用户原有全局配置没有被永久修改。

## 第一项实现任务

不要先实现所有 Adapter。按以下顺序：

1. 定义四个 v1alpha1 Schema；
2. 使用临时目录建立 Fixture Harness；
3. 定义 instruction、skill、mcp_server 三个 Ontology Entry；
4. 实现确定性 Scan 与 Inventory JSON；
5. 实现 Profile Composition 与 Provenance；
6. 实现 Drift Classification；
7. 选择最容易隔离运行的一个 TUI Adapter；
8. 演示 Personal + Company + Team + Project 的端到端组合。

开始大量编码前，必须先产出：

- Runtime Isolation ADR；
- 三个 TUI、三个初始 Concept 的 Precedence Matrix；
- Example Profiles；
- Expected Effective Harness；
- 需要针对已安装 CLI 实验验证的 Known Unknowns。

## 产品定位

这不是另一个 MCP Manager 或配置复制器。

> Compose the right AI coding harness for every repository, detect configuration drift across Claude Code, Codex, and OpenCode, and keep workflow assets portable without installing everything globally.

暂定 CLI 名称：`harnessctl`。正式命名前保持可替换。
