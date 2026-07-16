# oh-my-code-agent 项目初始化与端到端设计

## 1. 项目目标

构建一个 local-first、vendor-neutral 的 Coding Agent Harness 控制面。它面向
Claude Code、OpenAI Codex、OpenCode 等宿主，统一回答三个问题：

1. **Observe**：当前机器、用户、团队和项目实际存在什么配置，宿主可能加载什么；
2. **Model**：这些厂商配置在统一 Ontology 中分别意味着什么，哪些语义可以证明等价；
3. **Reconcile**：哪些配置由 Harness 托管，如何安全地让实际状态收敛到期望状态。

```text
Observe -> Model -> Reconcile
观测       建模       托管收敛
```

`Plan`、`Apply` 和 `Rollback` 是 Reconcile 的内部事务步骤，不是用户需要学习的产品概念。
`Verify` 是附着在每项结论上的证据等级，`Enforce` 是附着在每项策略上的保证等级，也不是额外流水线阶段。

项目首批端到端支持 Claude Code、OpenAI Codex 与 OpenCode。Cursor、GitHub
Copilot、Antigravity CLI、Pi、OpenClaw 和 Hermes 先进入版本化知识库与只读
Inventory，再按 capability 逐项晋级，不能使用一个含糊的“支持某宿主”承诺。

## 2. 产品边界

Harness 是由 Context、Profiles、Policies 和宿主能力共同计算出的 Effective State，
不是永久散落在全局目录中的一套复制文件。

产品负责：

- 广泛但安全地发现 Coding Agent 配置、能力和来源；
- 保留原始表示、未知字段、来源、版本和证据；
- 将可理解的观测事实投影到 Ontology；
- 组合个人、公司、团队、项目和临时任务的期望状态；
- 按根因聚合 Drift，而不是向用户展示配置叉乘；
- 对已验证的能力生成 dry-run、执行原子修改、验证和回滚；
- 为人和任意 LLM 提供同一套结构化查询与修复协议。

产品不负责：

- 保证不同模型或宿主产生相同行为；
- 把所有宿主压缩成最低公共子集；
- 猜测未公开或未通过 fixture 证明的优先级；
- 将 Instructions 伪装成安全强制策略；
- 自动翻译或执行任意 Plugin、Extension、Hook 代码；
- 接管 OAuth token、会话历史、密钥或账号级授权状态；
- 用 LLM 直接修改原生配置文件。

## 3. 产品原则

1. 以 logical entity 和 policy intent 为中心，不以文件为中心。
2. Observe 追求全面和无损；Model 允许局部和未知；Reconcile 必须保守和可证明。
3. Ontology 取能力并集，厂商特有能力保存在 `vendorExtensions`，不能静默丢失。
4. Scope 是带约束的组合图，不是假设固定顺序的四层继承树。
5. 每个 concept 和 field 有自己的 identity、merge、precedence 与 enforcement 语义。
6. 宿主优先级是版本化 resolver program，不是一个全局 `rank`。
7. `UNKNOWN`、`PARTIAL`、`OPAQUE` 和 `UNSUPPORTED` 是正常结果，不能包装成 Managed。
8. 只读路径不得依赖远端 LLM，也不得执行发现到的 Hook、MCP、Plugin 或 Extension。
9. 所有写操作必须支持 dry-run、compare-and-swap、备份、回滚和 Ledger。
10. Generated Artifact 是可查询构建产物，不能反向成为隐式真源。
11. 用户默认只看意图、根因、影响范围、样例和动作；宿主优先级只进入 Explain/Debug。
12. 第三方知识必须带版本和证据；未验证的新版本 fail closed 到只读模式。

## 4. 四个控制面

```text
┌────────────────────────────────────────────────────────────┐
│ Desired State                                              │
│ Profiles · Bindings · Policies · Exceptions · Assets       │
├────────────────────────────────────────────────────────────┤
│ Ontology and Knowledge                                     │
│ Concepts · Mappings · Host versions · Evidence · Fixtures  │
├────────────────────────────────────────────────────────────┤
│ Reconciliation                                             │
│ Inventory · Effective State · Drift · Plans · Artifacts    │
├────────────────────────────────────────────────────────────┤
│ Assurance                                                  │
│ Evidence · Verification · Guarantees · Ledger · Rollback   │
└────────────────────────────────────────────────────────────┘
```

Desired State 只表达用户意图，不包含 `~/.claude`、`.codex` 等厂商路径。
Knowledge 记录可升级的第三方事实。Adapter 将两者编译为宿主产物。Assurance 为每个结论附加证据和保证，不另造一份状态真源。

## 5. Scope、Profile 与 Policy

初始 scope：

```text
personal:<identity>
company:<organization>
team:<team>
project:<repository>
task:<temporary-purpose>       # Post-MVP
```

它们不是固定的覆盖链。一次 Context 可以同时命中多个团队、环境和工作区：

```yaml
personal: wangzitian
companies: [sea]
teams: [marketplace-buyer, infra]
project: order-service
workspace: /work/order-service
environment: staging
invocation:
  host: codex
  surface: cli
  cwd: /work/order-service/api
  profile: pragmatic
```

组合通过 concept-specific operations 表达：

```text
SET            设置标量值
UNSET          删除继承值
APPEND         有序追加
INCLUDE        按 logical ID 加入实体
EXCLUDE        按 logical ID 移除实体
DENY           形成不可被低层削弱的约束
LOCK           锁定字段或允许范围
PRESERVE       保留厂商原生表示
```

因此公司可以锁定网络策略，项目可以增加 Skill，个人可以保留语言和 UI 偏好；这些行为不需要用户理解各宿主的文件优先级。

### 5.1 Context 与 Binding 解析

Context discovery 可以使用当前 Git root、remote、目录祖先、`.harness/project.yaml`、
组织 repository registry、环境 marker 和 CLI 显式覆盖。选择顺序只用于定位 Binding，
不等于各 concept 的配置优先级：

```text
explicit CLI context
  -> project declaration
  -> signed organization/team registry
  -> local personal mapping
  -> safe auto-detection
```

如果 remote、嵌套 repository、worktree 或 submodule 产生多个合法 project identity，
系统必须显示候选并要求选择，不能静默绑定错误 Profile。一次确认可以保存为 local
mapping，但不得写回共享项目配置。

## 6. Ontology 与第三方 Knowledge

Ontology 是稳定的 canonical vocabulary 和关系模型。Normalization 是把部分
Observation 投影进 Ontology 的过程，两者不是两份概念系统。

每个 concept 至少定义：

- stable concept ID 和 schema version；
- logical identity 与 collision 规则；
- canonical fields 和 constraints；
- merge operators；
- portability classification；
- 可接受的 evidence 和 verification 方法；
- vendor extension 边界。

每个宿主事实必须独立版本化，至少记录：

- host、surface、version range 和 platform；
- discovery roots、trust gate 和 invocation context；
- precedence program 与 merge operator；
- capability matrix；
- 官方文档 URL、schema 或源码 revision；
- observed time、recheck time 和 confidence；
- fixture、probe 和 golden output；
- 已知 lossy mapping、unknown 与 retirement 状态。

示例 Knowledge Pack：

```yaml
apiVersion: harnessctl.dev/v1alpha1
kind: HostKnowledge

metadata:
  id: cursor:cli:3.9
  host: cursor
  surface: cli
  versionRange: ">=3.9.0 <4.0.0"
  observedAt: 2026-07-16
  recheckAfter: 2026-08-16

evidence:
  - kind: official-doc
    url: https://docs.cursor.com/context/rules
    digest: sha256:...
  - kind: executable-fixture
    path: fixtures/cursor/3.9/rule-conflict
    digest: sha256:...

capabilities:
  instruction:
    discover: EXACT
    parse: EXACT
    resolve: PARTIAL
    render: PARTIAL
    verify: PARTIAL
    reconcileMode: PATCHED
    verificationMethods: [static-resolver]
    guarantee: ADVISORY

precedencePrograms:
  - id: cursor.instructions.applicable-rules
    operator: CONCAT_APPLICABLE
    conflict: UNSPECIFIED
```

Knowledge 状态：

```text
FRESH       版本与证据在验证窗口内
DUE         到达复查时间，现有已验证版本仍可使用
STALE       检测到宿主新版本或证据变化，禁止扩大写入
CONFLICTED  文档、schema、源码或 probe 互相冲突
RETIRED     不再支持新生成，只保留历史解释能力
```

`latest` 只能用于发现更新，不能作为 Adapter 的事实依赖。运行时必须解析到不可变
Knowledge Pack ID 和 digest。

### 6.1 Knowledge 更新工作流

```text
Poll official sources
  -> detect candidate change
  -> create KnowledgeCandidate
  -> diff facts and affected capabilities
  -> run qualification fixtures
  -> human review
  -> publish immutable Knowledge Pack
  -> adapters opt in by version range
```

更新任务只能产生候选 PR，不能自动把 `UNKNOWN` 提升为 `MANAGED`。官方文档优先；
官方 schema 和源码可补足文档；社区信息只能触发调查；runtime probe 必须绑定宿主版本、平台和 fixture。

## 7. Capability 与 Assurance

不能用 `supports: true` 表达一个宿主。每个 `host × surface × version × concept`
分别声明操作能力：

```text
discover -> parse -> normalize -> resolve -> render -> verify -> enforce
```

每项操作使用以下支持等级：

```text
EXACT        语义和 round trip 已证明
COMPATIBLE   行为兼容，但原生表示不同
PARTIAL      只覆盖已声明字段或场景
OPAQUE       只保留位置、hash 和原始块
UNKNOWN      缺少足够证据
UNSUPPORTED  宿主没有该能力
```

Reconcile mode 由 capability 自动决定：

```text
MANAGED      可以生成、修改并验证
PATCHED      只修改明确拥有的字段
OBSERVED     只扫描和报告
OPAQUE       不解析内容，只保留和追踪
BLOCKED      语义未知，禁止 Apply
```

### 7.1 Evidence Level

```text
E0 DISCOVERED          找到物理来源
E1 PARSED              无损解析或安全保留
E2 RESOLVED            版本化 resolver 得到 effective value
E3 HOST_REPORTED       宿主原生命令或诊断接口确认
E4 BEHAVIOR_PROBED     隔离会话中的行为探针符合预期
E5 EXTERNALLY_PROVEN   OS、企业策略或独立审计系统证明
```

### 7.2 Guarantee Level

```text
HARD        OS sandbox、企业控制或权限系统阻止违规
RECONCILED 周期扫描并恢复，期间仍可能短暂 Drift
ADVISORY    依赖 Instruction 或模型服从
OBSERVED    只能发现，不能控制
```

Canary Probe 只能提升验证证据，不能把 `ADVISORY` 提升为 `HARD`。探针必须在临时
HOME、临时 workspace、随机 nonce 和全新 session 中运行，不得使用秘密，不得污染真实项目。优先使用宿主 introspection、status/debug API 和 prompt assembly 日志，只有缺少原生证据时才使用行为探针。

## 8. 配置组织方式

### 8.1 用户与组织配置

```text
~/.config/harnessctl/
├── config.yaml
├── sources.yaml
├── profiles/
│   ├── personal/
│   ├── company/
│   ├── team/
│   └── task/
├── bindings/
├── policies/
├── exceptions/
└── assets/
    ├── instructions/
    ├── skills/
    └── mcp/
```

公司和团队 Profile 可以来自受控 Git repository 或 package source，`sources.yaml`
只保存来源、版本和签名策略。Secret 只能保存引用。

### 8.2 项目配置

```text
<project>/.harness/
├── project.yaml
├── profiles/
├── policies/
├── exceptions/
└── assets/
```

项目只声明 profile binding、项目资产、参数和例外，不写宿主路径：

```yaml
apiVersion: harnessctl.dev/v1alpha1
kind: ProjectHarness

metadata:
  id: project:order-service

profiles:
  - personal:wangzitian
  - company:sea
  - team:marketplace-buyer

desired:
  instructions:
    include: [order-service-development]
  skills:
    include: [order-state-machine]
    exclude: [unrelated-finance-workflow]
  mcpServers:
    include: [codegraph]

exceptions:
  - ref: exception:sandbox-lab-network

vendorExtensions:
  cursor: {}
```

### 8.3 State、Cache 与不可变产物

```text
~/.local/state/harnessctl/
├── ledger/
├── backups/
└── ownership/

~/.cache/harnessctl/
├── knowledge/
├── observations/
└── runs/<run-id>/
    ├── manifest.yaml
    ├── inventory.json
    ├── effective.json
    ├── drift.json
    ├── plan.json
    ├── evidence.json
    ├── provenance.json
    └── artifacts/<host>/<surface>/
```

每个 Artifact 必须有稳定 URI、input/expected/observed digest、Adapter 与 Knowledge
版本、ownership 和 Plan ID。计划执行使用 source digest 做 compare-and-swap，避免覆盖 plan 生成之后发生的手工修改。

## 9. Observe

Observe 追求覆盖面，但不等于任意遍历用户目录。Adapter 只扫描已知配置根、显式
source 和安全的宿主状态接口，并且发现不授权执行。

Observation 至少包含：

```yaml
id: observation://order-service/codex/instruction/root-agents
host: codex
surface: cli
hostVersion: 0.144.5
invocationContext:
  cwd: /work/order-service
source:
  kind: file
  path: /work/order-service/AGENTS.md
scope:
  kind: workspace
rawDigest: sha256:...
parsedDigest: sha256:...
ownership: observed
opaqueVendorFields: {}
evidenceLevel: E1
```

观测覆盖率分别报告 source discovery、parse、normalize、resolve 和 runtime verification，不能用一个总百分比掩盖能力缺口。

## 10. Model

Model 阶段生成三张图：

1. **Observed Graph**：物理来源、内容、宿主、scope、trust 和 provenance；
2. **Effective Graph**：在给定 invocation context 下，宿主预计或确认加载的值；
3. **Desired Graph**：Profiles、Policies、Bindings 和 Exceptions 组合出的期望状态。

状态主键不是简单的 `Project × TUI`，而是：

```text
Project × Host × Surface × Version × Concept × Invocation Context
```

工具负责计算和缓存这个叉乘。用户界面只显示 logical entity、有效来源和根因。

Composition 不能统一使用 Last Write Wins：

```yaml
mergeStrategies:
  skills: UNION_BY_LOGICAL_ID
  mcpServers: MERGE_BY_LOGICAL_ID
  instructions: CONCAT_ORDERED
  permissions: DENY_WINS
  preferences: NEAREST_SCOPE_WINS
  vendorExtensions: PRESERVE_PER_HOST
```

宿主 resolver 还需要支持 `REPLACE`、`DEEP_MERGE`、`FIRST_MATCH`、`KEEP_BOTH`、
`NAMESPACE`、`ALL_RUN`、`LAST_MATCH`、`MANAGED_GUARDRAIL` 和 `UNSPECIFIED`。

## 11. Drift 与用户报告

Drift 先归一化为 assertion：

```text
entity_id + field + expected + observed + root_cause + remediation
```

然后按 `root_cause + remediation + outcome + adapter version` 聚合。复杂度为：

```text
机器成本 = Projects × Hosts × Concepts × Contexts
人类成本 ≈ 根因数 + Adapter 缺口数 + 显式例外数
```

Drift 类别：

```text
CONFIG_DRIFT       可管理配置与期望不同
EFFECTIVE_DRIFT    宿主有效状态与期望不同
SOURCE_DRIFT       同一 logical entity 的物理表示不同
CAPABILITY_GAP     Adapter 缺少解释、写入或验证能力
KNOWLEDGE_DRIFT    宿主版本或第三方事实超出已验证范围
EXCEPTION          已授权且未过期的差异
UNKNOWN            身份、语义或结果无法安全判断
```

默认报告只展示根因、影响范围、1 到 3 个覆盖不同 outcome 的确定性样例、建议动作、
Evidence 和 Guarantee。完整叉乘进入 `matrix`，原生优先级进入 `explain --trace`。

```text
DR-017  公司安全基线未完全应用                         HIGH

期望      workspace-write + on-request
来源      company/security-default
影响      8 projects · 5 hosts · 40 artifacts
证据      38 × E3, 2 × E2
保证      RECONCILED
建议      修改 38 个 artifact，保留 2 个有效 exception

样例
  infra2 / Codex       danger-full-access -> workspace-write
  finance / Claude     approval bypass    -> on-request
```

## 12. Reconcile

Reconcile 是确定性事务：

```text
Desired Graph + Effective Graph + Capability Manifest
  -> Change Set
  -> Dry-run Plan
  -> Policy and risk checks
  -> Human/automation approval
  -> Atomic Apply
  -> Verify
  -> Ledger or Rollback
```

Ownership：

```text
managed      Harness 拥有并可收敛
patched      Harness 只拥有已声明字段
observed     只报告，不写入
passthrough  解析时保留，输出时原样带回
external     由企业策略、MDM、其他工具或账号控制
```

正常启动优先使用宿主官方 config/home 参数、隔离 runtime directory 或可恢复 overlay。
默认禁止覆盖真实全局配置。若用户明确选择持久托管，Adapter 必须证明 lossless patch、
ownership boundary、backup 和 rollback。

MCP 在 MVP 中优先编译为宿主原生 registry。后续可以增加可选的 local gateway：

```text
Coding Hosts -> harnessctl MCP Gateway -> context-filtered servers and tools
```

Gateway 是独立 runtime capability，不能和原生 MCP 定义静默合并，也不能成为所有宿主必须使用的架构前提。

## 13. LLM 修复协议

任意 LLM 通过相同的 CLI JSON 或本地 MCP 读取 Observation、Drift、Explain 和 Plan。
LLM 只能提交 canonical `RepairProposal`：

```yaml
apiVersion: harnessctl.dev/v1alpha1
kind: RepairProposal

metadata:
  driftId: DR-017
  basedOnFingerprint: sha256:...

changes:
  - target: policy/company/security-default
    operation: SET
    path: spec.permissions.sandbox
    value: workspace-write
    rationale: Align managed company baseline
```

程序确定性负责 schema validation、capability gating、policy checks、render、diff、
apply、verify 和 rollback。LLM 不得直接写原生配置，也不能把行为探针结果升级成安全强制证据。

本地 MCP/CLI contract：

```text
harness.status
harness.observe
harness.drift
harness.explain
harness.matrix
harness.propose_repair
harness.plan
harness.apply
harness.verify
harness.knowledge_status
```

## 14. 建议仓库结构

```text
.
├── cmd/harnessctl/
├── internal/
│   ├── domain/                 # canonical types, IDs and invariants
│   ├── observe/                # discovery orchestration and inventory
│   ├── model/                  # normalization and graph construction
│   ├── profiles/               # profiles, bindings, policies, exceptions
│   ├── resolve/                # desired/effective resolvers
│   ├── drift/                  # assertions, root-cause grouping, reports
│   ├── reconcile/              # plans, apply, rollback and ownership
│   ├── assurance/              # evidence, verification and guarantees
│   ├── knowledge/              # immutable packs and update candidates
│   ├── artifact/               # manifests, provenance and content store
│   ├── runtime/                # isolated views and host launch
│   ├── report/                 # human, JSON and TUI projections
│   └── adapters/
│       ├── claude/
│       ├── codex/
│       └── opencode/
├── ontology/
│   ├── concepts/
│   ├── operators/
│   └── schemas/
├── knowledge/
│   ├── sources.yaml
│   └── hosts/<host>/<surface>/<version>/
│       ├── manifest.yaml
│       ├── capabilities.yaml
│       ├── precedence.yaml
│       └── evidence.yaml
├── schemas/
│   ├── config/
│   ├── domain/
│   └── protocol/
├── profiles/examples/
├── fixtures/<host>/<version>/<case>/
├── docs/
│   ├── ontology/
│   ├── architecture/
│   └── operations/
├── tests/
└── README.md
```

优先实现单一跨平台 Go 二进制。项目主要复杂度来自文件系统、schema、图解析、
内容寻址、进程隔离和可复现测试，不需要提前引入多服务控制面。

## 15. 核心接口草案

Adapter 只负责宿主物理语义，不承担 Profile 组合和跨宿主 Drift：

```go
type HostAdapter interface {
    ID() AdapterID
    Detect(context.Context, DetectRequest) ([]HostInstance, error)
    Capabilities(context.Context, HostInstance) (CapabilityManifest, error)
    Observe(context.Context, ObserveRequest) (ObservationSet, error)
    Resolve(context.Context, ResolveRequest) (HostEffectiveState, error)
    Render(context.Context, RenderRequest) (ArtifactSet, error)
    Verify(context.Context, VerifyRequest) (EvidenceSet, error)
    Launch(context.Context, LaunchRequest) error
}

type KnowledgeRepository interface {
    Resolve(context.Context, HostInstance) (KnowledgePack, error)
    Status(context.Context, KnowledgeQuery) ([]KnowledgeStatus, error)
    ProposeUpdate(context.Context, UpdateRequest) (KnowledgeCandidate, error)
}

type Normalizer interface {
    Normalize(context.Context, ObservationSet, KnowledgePack) (ObservedGraph, error)
}

type ProfileResolver interface {
    Resolve(context.Context, InvocationContext, []Profile) (DesiredGraph, error)
}

type DriftEngine interface {
    Compare(context.Context, DesiredGraph, EffectiveGraph) (DriftReport, error)
    Explain(context.Context, ExplainQuery) (Explanation, error)
}

type Reconciler interface {
    Plan(context.Context, ReconcileRequest) (Plan, error)
    Apply(context.Context, Plan, ApplyOptions) (ApplyResult, error)
    Rollback(context.Context, LedgerEntry) (RollbackResult, error)
}

type AssuranceEngine interface {
    Verify(context.Context, VerifyTarget) (EvidenceSet, error)
    ClassifyGuarantee(context.Context, PolicyOutcome) (GuaranteeLevel, error)
}
```

关键请求必须显式传入 `InvocationContext`、Adapter version、Knowledge digest 和输入
fingerprint，禁止函数隐式读取“当前最新”事实。

## 16. CLI 与 TUI 信息架构

```bash
harnessctl observe [--host codex] [--json]
harnessctl status
harnessctl drift [--project <id>]
harnessctl drift show <drift-id>
harnessctl explain <project> <host> <concept> [--trace]
harnessctl matrix <drift-id>
harnessctl plan [--drift <id>]
harnessctl apply <plan-id>
harnessctl verify <artifact-or-policy>
harnessctl run <claude|codex|opencode>
harnessctl knowledge status
harnessctl knowledge update --dry-run
```

TUI 默认按根因组织：

```text
Workspace
├── Drift
├── Plans
├── Profiles
├── Bindings
├── Exceptions
└── Debug
    ├── Effective State
    ├── Host Matrix
    ├── Precedence Trace
    └── Native Artifacts
```

默认页面禁止展示厂商目录和 rank。用户选择 `Explain` 后才展开：effective value、
selected source、ignored sources、policy constraint、Adapter rule、Artifact URI 和 evidence。

## 17. 安全与正确性

- 只读命令绝不修改源文件或执行发现到的配置；
- Scanner 不读取 keyring、OAuth token、session transcript 和 saved approval；
- Secret reference 与普通配置分开，日志、Diff、Provenance 和 LLM context 全部脱敏；
- 未验证宿主版本、`UNKNOWN` precedence 和 lossy round trip 禁止 Apply；
- 所有写入使用 atomic write、source digest compare-and-swap、backup 和 Ledger；
- 未知 Vendor Field 必须保留；
- symlink、跨 filesystem、文件权限和 owner 变化必须进入 Plan；
- 权限扩大、网络开放、Hook/MCP/Extension 启用必须单独确认；
- 公司 deny/lock policy 不能被团队、项目、个人或 LLM proposal 削弱；
- Probe 只能在隔离 fixture 中运行，结果标记为行为证据而非强制保证；
- Generated Artifact 必须可重建、可查询、可追溯且不会成为 canonical source。

## 18. 测试与 Adapter Qualification

每个可写 capability 必须通过版本化 qualification suite：

1. User、Project、Local、CLI 和 managed policy 同时存在；
2. 同名 Instruction、Skill、MCP、Agent 或 Hook 冲突；
3. 未知字段、注释和顺序的无损 round trip；
4. Plan 与 Apply 之间发生手工修改；
5. 宿主版本升级后 discovery 或 precedence 变化；
6. 无法查询 effective state；
7. 企业策略禁止本地值；
8. 多个项目共享一个全局 source；
9. 临时 HOME、不同 cwd、profile 和 CLI flags；
10. Secret redaction 与只读不写入证明。

核心 invariants：

```text
相同输入和 Knowledge digest 产生相同 fingerprint
observe 不写入、不执行
unknown fields 在声明 lossless 的 round trip 中不丢失
deny/lock policy 不能被低保证层削弱
每个 effective value 有 provenance 和 evidence level
Apply 只接受未过期且 source digest 匹配的 Plan
未验证的新宿主版本自动降级而不是继续写入
同一根因的完整矩阵始终可从报告追溯
```

## 19. 端到端项目计划

### M0: Contracts and Qualification Lab

交付：

- 冻结 `Profile`、`ProjectHarness`、`Observation`、`KnowledgePack`、`Plan` 和 `Evidence` v1alpha1 schema；
- 建立 Claude、Codex、OpenCode 的临时 HOME fixture harness；
- 为 Instructions、Skills、MCP 建 precedence 和 collision fixtures；
- 产出 Runtime Isolation ADR、Ownership ADR 和 Knowledge Update ADR。

退出条件：任何未验证 precedence 都返回 `UNKNOWN`；所有 read-only fixture 证明零写入。

### M1: Observe

交付：

- 检测宿主 binary、version、surface、home、workspace、trust 和 invocation context；
- 扫描三个首批宿主的 Instructions、Skills 和 MCP；
- 输出 lossless Inventory、opaque facts、source digest 和 coverage；
- 提供 `observe`、`status` 和 JSON contract。

退出条件：同一 fixture 扫描结果确定，Secret 不进入输出，未知内容不丢失。

### M2: Model

交付：

- 实现 Ontology normalization、logical identity 和 ambiguity handling；
- 实现 Personal、Company、Team、Project Profile 与 Binding；
- 构建 Observed、Effective、Desired 三张图；
- 每个 effective value 输出 provenance、capability、evidence 和 guarantee。

退出条件：四层组合和多团队冲突可解释；不存在统一 Last Write Wins 捷径。

### M3: Drift and Explain

交付：

- 生成 Config、Effective、Source、Capability 和 Knowledge Drift；
- 按根因聚合，提供代表样例和完整 matrix；
- 实现 `drift`、`explain`、`matrix` 与 immutable run manifest；
- 生成供任意 LLM 使用的脱敏 Repair Context。

退出条件：`N` 个项目和 `M` 个宿主的同一根因默认只产生一张人类 action card，所有叉乘单元仍可查询。

### M4: Reconcile One Host

交付：

- 选择 qualification 最完整的一个宿主；
- 实现 capability-gated Plan、patch/render、CAS Apply、Verify、Ledger 和 Rollback；
- 支持隔离 Runtime View 和 `run`；
- 对持久托管文件实现 ownership boundary。

退出条件：dry-run 与 Apply 内容一致；并发手工修改会阻止 Apply；失败可恢复；原始全局配置不被隐式覆盖。

### M5: Multi-host Managed Capabilities

交付：

- 将已证明的 Instructions、Skills、MCP capability 扩展到另外两个首批宿主；
- 标记 EXACT、COMPATIBLE、PARTIAL 和 vendor extension；
- 支持同一 Desired Graph 编译多个宿主 Artifact；
- 不支持的 operation 自动降级到 OBSERVED/BLOCKED。

退出条件：不以最低公共子集限制 Ontology，也不因一个宿主缺失能力而伪造等价配置。

### M6: Knowledge Lifecycle

交付：

- 监控官方文档、schema、release 和源码 revision；
- 生成 KnowledgeCandidate PR 和影响分析；
- 自动运行受影响 fixture，人工批准 Knowledge Pack；
- 对未验证新版本产生 Knowledge Drift 并停止扩大写入。

退出条件：第三方升级不会静默改变 Reconcile 行为，历史 Run 仍能解析到原 Knowledge digest。

### M7: TUI and Agent Protocol

交付：

- 按根因呈现 Drift、Plan、Profiles、Bindings、Exceptions 和 Debug tree；
- 提供稳定 CLI JSON 与本地 MCP；
- 允许任意 LLM 提交 RepairProposal；
- 支持在用户批准一次后展开并执行完整 Change Set。

退出条件：普通用户完成日常 Drift 修复不需要理解宿主路径和优先级；高级用户可以从任一 action card 追到原始 Artifact 和 evidence。

## 20. MVP 验收场景

在包含 `.harness/project.yaml` 的仓库执行：

```bash
harnessctl observe
harnessctl drift
harnessctl explain order-service codex instruction
harnessctl plan
harnessctl apply <plan-id>
harnessctl verify <artifact-id>
harnessctl run codex
```

必须得到：

1. Global、Project、Invocation Context 的无损 Inventory；
2. Personal、Company、Team、Project 组合出的 Desired Graph；
3. 带 Knowledge digest 的 Effective Graph；
4. 根因聚合的 Drift 报告和可展开完整矩阵；
5. 每个值的 provenance、Evidence Level 和 Guarantee Level；
6. capability-gated dry-run、原子 Apply、Verify、Ledger 和 Rollback；
7. 使用隔离 Runtime View 启动的至少一个宿主；
8. 用户原有全局配置没有被隐式或不可恢复地修改；
9. 未验证版本和未知 precedence 明确降级为只读；
10. 任意 LLM 能通过稳定协议提议修复，但不能绕过 deterministic checks。

## 21. 第一批实现任务

1. 建立 v1alpha1 domain schemas 和 stable ID 规则；
2. 建立 Claude、Codex、OpenCode 的 Adapter Qualification fixture；
3. 将现有 Markdown Ontology 拆成 concept schema 与版本化 Host Knowledge Pack；
4. 实现只读 Observe 和 immutable Run Manifest；
5. 实现三图模型、Profile Resolver 和 provenance；
6. 实现 root-cause Drift、样例选择和 matrix query；
7. 选择一个 capability 最完整的宿主完成 Reconcile；
8. 证明 Plan、Apply、Verify、Rollback 和 Knowledge 升级降级路径。

开始大量 Adapter 编码前，必须先完成：

- 三个首批宿主、三个首批 concept 的 executable precedence fixtures；
- 配置 ownership 和 lossless patch contract；
- Knowledge Pack 更新与过期策略；
- 一份跨 Personal、Company、Team、Project 的端到端 golden scenario；
- 一份根因报告到原生 Artifact 的完整 Explain trace。

## 22. 产品定位

这不是另一个 MCP Manager，也不是通用配置复制器。

> Observe every coding-agent harness, model what can be proven, and reconcile only what can be managed safely.

暂定 CLI 名称为 `harnessctl`，正式命名前保持可替换。
