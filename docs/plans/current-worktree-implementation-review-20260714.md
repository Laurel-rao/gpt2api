# 当前工作区实现逻辑复盘与代码批注

复盘日期：2026-07-14

## 1. 复盘边界与证据规则

本文覆盖当前工作区 13 个未提交文件，以及这些文件实际调用到的前后端实现：

- 配置：`internal/settings/model.go`
- 测速脚本：`scripts/net-speed-test.mjs`、`scripts/package.json`、`scripts/README.md`
- 画布组件：`VideoWorkflowCanvas.vue`、`VideoWorkflowLibrary.vue`、`VideoWorkflowNodeCard.vue` 及对应测试
- 图逻辑：`videoWorkflowGraph.ts`、`videoWorkflowElk.ts` 及对应测试
- 页面编排：`web/src/views/personal/VideoWorkflows.vue`

为确认真实行为，继续追踪了以下未改动代码：

- 页面路由和权限：`web/src/router/index.ts`、`internal/server/router.go`、`internal/rbac/menu.go`
- 前端接口契约：`web/src/api/videoWorkflow.ts`
- 后端更新、修订和运行：`internal/videoworkflow/handler.go`、`service.go`、`dao.go`、`runtime.go`
- 图迁移、布局和展示辅助：`videoWorkflowLayout.ts`、`videoWorkflowPresentation.ts`

文中“批注”只记录能够由现有代码直接证明的问题；没有代码证据的推测不列入结论。

## 2. 整体入口和权限链

1. 前端路由 `/personal/video-workflows` 懒加载 `VideoWorkflows.vue`，路由元数据要求 `self:video_workflow` 且 `adminOnly: true`，见 `web/src/router/index.ts:35-39`。
2. 后端同样对工作流、运行和素材三组接口同时应用当前管理员校验和 `PermSelfVideoWorkflow` 权限，见 `internal/server/router.go:196-225`。
3. 因此前端菜单隐藏不是安全边界；真正的访问边界位于 Gin 路由中间件。

后端接口分为三组：

- `/api/me/video-workflows`：模板、模型、工作流 CRUD、校验、估价、运行历史和启动运行。
- `/api/me/video-workflow-runs`：运行详情、取消、角色审批和分镜审批。
- `/api/me/video-assets`：素材列表、上传、删除、签名和图片变换。

## 3. 页面初始化和工作区加载

### 3.1 初始化

`bootstrap()` 在 `VideoWorkflows.vue:903-923` 并发请求模板、工作流列表、前 100 个素材和可用视频模型：

- 模板或工作流主请求失败：进入离线分支，`backendAvailable=false`，再调用 `loadWorkspace(null)`。
- 素材和模型请求失败：各自降级为空数组，不阻断主工作区。
- 模型接口返回空数组：保留页面已有默认模型。
- 模板加载完成后：默认选择第一项模板，再加载工作区。

### 3.2 工作区切换

`loadWorkspace()` 会增加 `workspaceGeneration`，后续异步返回都用该世代判断结果是否仍属于当前工作区。切换时会清空选择、历史详情、当前运行、轮询状态和 stale 本地世代。

有服务端工作流时：

- 图先经过 `migrateVideoWorkflowGraph()` 升级为可编辑 Graph v2。
- 查询最新一条运行并设为 `activeRun`。
- 恢复本地草稿时仍以工作流 ID 和 revision 隔离。

切换已有工作流前，`selectWorkflow()` 先调用 `flushSave()`；保存失败或发生修订冲突时取消切换，见 `VideoWorkflows.vue:1145-1149`。

## 4. Graph 数据模型与迁移

Graph 的真实契约定义在 `web/src/api/videoWorkflow.ts:3-134`：

- 全局设置：画幅、分辨率、30fps、15 秒场景时长、审批策略和默认模型。
- 节点：位置、位置模式、分组场景、输入输出端口、启停、锁定、素材绑定和展示状态。
- 边：源节点/端口到目标节点/端口，可附带手工曲线或自动布局路由。
- 场景组：固定 15 秒，记录成员节点、边界、启停和折叠状态。
- 时间线：Graph v2 使用 `clips`；`clip_node_ids` 仅用于 Graph v1 兼容。

`migrateVideoWorkflowGraph()` 的行为不是简单类型转换：

1. 非对象输入直接回退到 starter graph。
2. 深拷贝原始 JSON，规范全局设置、节点和图片变换。
3. 将旧 `clip_node_ids` 转换为带 `trim_in_ms`、`trim_out_ms` 的 clips。
4. 根据 clips 重建时间线输入端口和指向时间线的边。
5. 保留合法的共享角色总线布局锚点。

图校验同时检查节点/边上限、ID 唯一性、端口存在性、端口类型、单值输入、环、角色/场景数量、时间线/合成节点数量、裁剪范围和 100ms 对齐。

> **[批注 P2｜前端完整校验漏掉时间线 clip 与实际连线的一致性]**
>
> 证据：前端 `validateVideoWorkflowGraph()` 在 `videoWorkflowGraph.ts:751-780` 校验 clip 的来源节点、输出类型和裁剪范围，但没有确认 Graph 中存在对应的 `source_node_id/source_port → timeline` 边；后端 `ValidateGraph()` 在 `internal/videoworkflow/graph.go:234-264` 明确构造 `connectedSources`，并在 `requireComplete=true` 时拒绝没有对应边的 clip。
>
> 触发：时间线配置保留 clip，但对应入边因异常编辑、导入或不完整变更而缺失。
>
> 结果：前端运行前本地校验通过，随后服务端校验失败；前后端显示的“可运行”结论不一致。
>
> 完整实现要求：前端 `requireComplete` 分支补齐与后端相同的 clip 来源连线校验，并增加契约测试锁定两端规则。

## 5. 画布展示同步

`syncFlow()` 将持久化 Graph 转换成 Vue Flow 的临时展示模型：

1. 根据 `edgeDisplayMode` 计算共享角色总线及被折叠的逻辑边。
2. 将 scene group 转成不可拖动的 zone 节点。
3. 将 Graph 节点经 `displayNode()` 合并素材绑定和最近一次运行状态。
4. 将逻辑边转换为普通边、可调曲线边或共享总线汇总边。
5. 选择节点或边时提高相关边透明度，其余边按 `smart/all/hidden` 降低透明度。

场景启停展示与后端 `activeNodeIDs()` 对齐：

- `group.enabled=false`：后端停用组内非 timeline/compose 节点。
- 单节点 `enabled=false`：后端停用该节点。
- 停用状态沿出边继续传播到下游非 timeline/compose 节点，见 `internal/videoworkflow/runtime.go:750-783`。
- 前端 zone 仅在 group 启用且组内至少一个可编辑节点启用时显示“已启用”，见 `VideoWorkflows.vue:577-584`。

## 6. 左右面板、重命名和选择状态

### 6.1 左侧节点库折叠

`VideoWorkflowLibrary.vue` 新增 `close` 事件和 `node-library` DOM ID。父页面用 `panelLayout.libraryOpen` 控制：

- 打开：左列宽度为保存值，显示节点库和左侧拖拽条。
- 关闭：CSS 变量 `--left-panel` 变成 `0px`，显示画布左上角恢复按钮。
- `libraryOpen` 与左右栏宽度、检查器状态、连线显示模式、自动排布开关一并存入 localStorage。
- 小于 1280px 时，媒体查询统一隐藏左右栏、拖拽条和恢复按钮。

### 6.2 选择同步

Vue Flow 的框选和多选首先更新 `selectedNodeIDs`。新逻辑在收到至少一个 `select` change 后同步单选 ID：

- 多选结果为空：清空 `selectedNodeID`。
- 原 Inspector 节点不在新多选集合：把最后一个选中节点设为 Inspector 当前节点。

这避免了“画布显示选中了新节点，但参数修改或单节点运行仍作用于旧节点”。

### 6.3 重命名

重命名不是独立字段更新。前端调用现有 `PUT /api/me/video-workflows/:id`，必须同时发送：

- 新名称；
- 当前 revision；
- 当前完整 graph。

后端 `UpdateWorkflow()` 校验草稿图后调用 DAO；DAO 用
`WHERE workflow_id=? AND user_id=? AND revision=?` 做乐观锁，并在成功时执行 `revision=revision+1`，见 `internal/videoworkflow/dao.go:158-182`。

> **[批注 P1｜重命名存在并发保存竞态且吞掉真实错误]**
>
> 证据：普通自动保存由唯一的 `activeSavePromise` 串行化，见 `VideoWorkflows.vue:971-1013`；`renameActiveWorkflow()` 在 `1152-1181` 直接再次调用 `updateVideoWorkflow()`，没有等待或复用 `activeSavePromise`。两个请求可携带同一个 revision 并发到达，后到者必然收到后端 409。该函数的 `catch { /* 取消 */ }` 又把取消、网络错误和 409 全部静默处理。
>
> 触发：用户修改画布后，在自动保存请求尚未完成时确认重命名；或另一个页面已推进 revision。
>
> 结果：重命名可能无提示失败；若重命名先成功，仍在途的自动保存会把页面置为“修订冲突”。
>
> 完整实现要求：重命名前先 `flushSave()`，或将名称变化并入同一保存队列；catch 只忽略 `cancel/close`，409 必须进入统一冲突处理。

## 7. 节点新增、复制、拖动和启停

### 7.1 新增和快速创建

- 节点库点击新增：调用 `makeVideoWorkflowNode()`，视频节点再应用当前可用模型。
- 自动排布开启或未提供明确坐标时，新节点的 `position_mode` 设为 `auto`。
- 节点写入 Graph 后同步选择，并在下一帧调用 Vue Flow `updateNodeInternals()` 刷新端口。
- 从输出端口拖到空白区域后选择节点类型：创建兼容输入节点并同时创建边。

### 7.2 复制粘贴

粘贴时为节点和边生成新 ID，只复制选中节点内部的边。新实现逐条调用 `connectionError()`，不合法边被跳过并汇总提示，避免把超限、重复、环或端口冲突直接写进 Graph。

### 7.3 拖动

- 拖动中只计算 6px 范围的横纵对齐辅助线。
- 拖动结束才把 Vue Flow 位置写回 Graph。
- 锁定节点不写回位置。
- 手动移动后设为 `position_mode=manual`，清除自动边路由并重算场景组边界。
- 原先 Alt+拖动复制入口已移除，复制保留为快捷键和其他现有入口。

### 7.4 节点和场景启停

`toggleEnabled()` 对选中且非 timeline/compose 的节点统一取反，并重新计算涉及场景组：

- 组内全部可编辑成员停用：`group.enabled=false`。
- 任一成员重新启用：`group.enabled=true`。
- 被停用的视频节点同时从时间线 clips 中移除。

该双写是必要的，因为后端同时读取 node.enabled 和 group.enabled。

## 8. 连线完整逻辑

### 8.1 端口方向规范化

Vue Flow 现在使用 `ConnectionMode.Loose`，允许从 target handle 反向拖到 source handle。`normalizeVideoWorkflowConnection()`：

1. 缺节点或缺 handle：返回 `null`。
2. source handle 属于源节点输出且 target handle 属于目标节点输入：保持原方向。
3. source handle 属于源节点输入且 target handle 属于目标节点输出：交换两端。
4. 两种端口关系都不成立：保留原值，让 `connectionError()` 返回具体的端口错误。

该函数同时用于新连线和已有边端点更新。

### 8.2 业务校验

`connectionError()` 按顺序检查：

- 64 条边上限；
- 自连接；
- 节点和端口存在；
- 端口类型一致；
- 完全重复边；
- 单值输入端口只能有一条边；
- 新边是否通过既有邻接关系形成环。

单值端口已有边时，页面要求用户确认替换，再在移除旧边的临时图上重新校验，校验通过后一次提交。

### 8.3 拖到节点主体

从输出 handle 开始拖动时保存 `pendingConnection`。结束位置不是 handle 时：

- 若落在普通节点主体：按输出类型寻找目标输入，找到后直接调用标准 `onConnect()`。
- 目标没有兼容输入：显示错误，再打开空白处快速创建菜单。
- 落在空白处：按鼠标屏幕坐标显示可创建的兼容节点类型。
- 左栏折叠时，菜单横坐标不再减去左栏宽度。

> **[批注 P2｜节点主体自动选端口不检查端口是否已占用]**
>
> 证据：`findCompatibleTargetPort()` 在 `VideoWorkflows.vue:1520-1526` 只按类型和共享总线折叠状态取第一个输入；输入占用检查直到 `onConnect()` 才发生。
>
> 触发：目标节点有两个同类型单值输入，第一个已有边、第二个为空；这类结构已真实存在于动态时间线端口和多角色图片输入。
>
> 结果：拖到节点主体会选择第一个已占用端口并弹出替换确认，空闲端口不会被优先使用。
>
> 完整实现要求：候选排序应为“可见且未占用、未占用、可见且已占用、已占用”，最后才进入替换确认。

> **[批注 P2｜connect-end 声明支持触摸事件，但坐标处理仅支持鼠标]**
>
> 证据：`onConnectEnd(event?: MouseEvent | TouchEvent)` 在 `VideoWorkflows.vue:1535-1567` 直接把事件断言成 MouseEvent，并读取 `clientX/clientY`；TouchEvent 的坐标位于 `changedTouches/touches`。
>
> 触发：触摸设备从输出端口拖到节点主体或空白处。
>
> 结果：快速创建菜单坐标和 `screenToFlowCoordinate()` 输入变成 `undefined/NaN`。
>
> 完整实现要求：显式提取 MouseEvent 或首个 changedTouch 的客户区坐标，并补触摸交互测试。

## 9. 自动布局和自动排布

### 9.1 ELK 输入

`videoWorkflowElk.ts` 把每个节点转换为带固定顺序 WEST 输入端口和 EAST 输出端口的 ELK 节点；scene group 转成父节点，未分组节点直接放在根层。边直接引用端口 ID，避免仅按节点中心布局。

本次增加的真实约束包括：

- 普通边间距和分层边间距；
- 边与节点的跨层间距；
- 强制节点模型顺序；
- LAYER_SWEEP 交叉最小化；
- NETWORK_SIMPLEX 节点放置；
- 端口居中；
- thoroughness=7。

ELK 返回后，`collectPositions()` 将组内相对坐标加上父组坐标，最终交给 `applyVideoWorkflowLayoutProposal()`；布局模块只应用当前模式允许移动的节点。

### 9.2 手动整理

- “整理自动节点”：只移动 `position_mode=auto` 且未锁定节点。
- “所选恢复自动布局”：先把选中未锁定节点改为 auto，再执行自动节点布局。
- “重新整理全部”：确认后重排所有未锁定节点，并把手工位置改回自动布局。
- 布局期间若 `changeSequence` 变化，结果丢弃，避免覆盖用户新修改。
- ELK 不可用时布局层回退到 dagre，非静默操作会提示。

### 9.3 持续自动排布

自动排布开关写入 localStorage。开启后：

1. 监听由节点 ID/折叠状态和边端点组成的结构签名。
2. 结构变化后防抖 480ms。
3. 正在布局时重新排队。
4. 对当前 Graph 深拷贝执行 auto 模式。
5. 布局完成前画布若变化，丢弃结果并再次排队。
6. 组件卸载时清理定时器。

位置变化不在结构签名中，因此布局结果写回不会自行触发无限布局循环。

## 10. 节点状态、运行展示和 stale 世代

### 10.1 展示状态

`activeRunNodeMap` 现在始终读取最近一次运行的 `node_runs`，不再要求运行 revision 与当前工作流 revision 完全一致。`displayNode()` 再通过 `resolveVideoWorkflowDisplayedStatus()` 合并 Graph 本地状态和运行状态：

- Graph 为 stale 时保持 stale，不让旧运行成功状态覆盖。
- 非 stale 节点可继续显示最近运行的进度、输出和错误。
- 保存导致 revision+1 后，刚生成的预览和进度不会瞬间消失。

严格 revision 对齐仍由 `activeRunMatchesWorkflow` 保留，只用于允许修改当前 Graph 的逻辑，例如清除 stale。

### 10.2 stale 标记

参数、模型或素材变化时：

1. 当前节点设为 stale 并记录原因。
2. `noteNodeStale()` 增加全局 `staleEpoch`，把节点 ID 映射到该世代。
3. `markDownstreamStale()` 按出边广度遍历，把所有下游节点标为 stale 并记录各自世代。
4. 启动运行成功后，`activeRunStaleEpoch` 记录运行启动时的世代。
5. 轮询只在 Graph 未 dirty 且运行 revision 对齐时处理 stale。
6. 仅成功 node run 且节点 stale 世代不晚于运行启动世代时清除 stale。

设计目标是避免“运行启动后用户再次编辑，旧运行轮询回来把新 stale 清掉”。

> **[批注 P1｜重新加载后已有 stale 永远不能被成功运行清除]**
>
> 证据：`loadWorkspace()` 在 `VideoWorkflows.vue:795-800` 清空 `nodeStaleEpoch`，但 Graph 中的 `node.status/stale_reason` 会从服务端草稿恢复；轮询在 `2158-2164` 遇到 `markedAt === undefined` 直接跳过。
>
> 触发：参数修改已自动保存，刷新页面或切换工作流再切回，然后对 stale 节点运行成功。
>
> 结果：运行成功且 revision 对齐后，节点仍持续显示“需更新”。
>
> 完整实现要求：加载工作区时为已有 stale 节点登记基线世代，或用“运行启动时 stale 节点集合 + 启动后新增世代”分别判断。

### 10.3 轮询失败

每次运行详情查询成功会清零连续失败计数。连续失败达到 3 次时只提示一次“运行状态刷新失败，正在重试”；后续仍按原轮询定时器继续查询。运行进入终态后停止轮询。

## 11. 运行、保存和后端状态机

### 11.1 保存

所有 Graph 编辑经 `markDirty()`：

- `dirty=true`；
- `changeSequence+1`；
- 写入 localStorage 草稿；
- 安排自动保存。

自动保存快照包含名称、当前 revision 和完整 Graph。服务端允许不完整草稿保存，但仍执行基础图校验；DAO 乐观锁成功后 revision+1。保存期间发生新编辑时，旧快照成功后不会清 dirty，而是继续安排下一次保存。

### 11.2 启动运行

`startRun()` 的顺序为：

1. 阻止并发运行准备动作。
2. dirty 时先 `flushSave()`，防止运行旧 revision。
3. 前端执行完整或局部图校验。
4. 调用服务端校验和估价。
5. 显示费用确认；确认期间 Graph、revision 或 changeSequence 变化即终止。
6. 带 revision、run mode、start node、estimate token 和 request ID 提交运行。
7. 后端再次校验 Graph、revision、运行选择和 estimate token。
8. 后端持久化不可变 Graph snapshot，再交给 runtime。

`request_id` 用于幂等。重复提交时，后端要求工作流、revision、模式、起始节点和估价信息都与原运行一致，否则返回 request conflict。

### 11.3 节点选择

后端 `selectedNodeIDs()`：

- full：选择所有 active 节点，并要求至少剩一个视频节点。
- node_only：选择起始节点及其全部可运行上游。
- downstream：先选择起始节点上下游闭包，再为已选下游补齐各自上游。
- 起始节点已停用或依赖停用链时拒绝运行。

## 12. 节点卡片和测试覆盖

节点卡片：

- 输入 handle 位于左侧，输出 handle 位于右侧。
- smart 模式把多个角色输入折叠为不可连接的共享角色展示 handle。
- 媒体节点优先展示真实生成输出；图片节点优先展示已绑定版本预览。
- 本次通过 card `position: relative`、handle `z-index: 6` 和 connectable handle 的 pointer-events 修复端口被节点主体遮挡的问题。

> **[批注 P2｜Graph v2 时间线摘要仍读取旧字段]**
>
> 证据：类型定义明确 `clips` 是新写入字段、`clip_node_ids` 只为 v1 兼容，见 `web/src/api/videoWorkflow.ts:38-42`；节点卡片摘要在 `VideoWorkflowNodeCard.vue:60-64` 只读取 `clip_node_ids.length`。
>
> 触发：合法 Graph v2 时间线只有 clips，没有兼容字段。
>
> 结果：卡片显示“0 个片段 · 单轨”。
>
> 完整实现要求：优先读取 `clips.length`，仅在 clips 不存在时回退到 `clip_node_ids.length`。

> **[批注 P3｜端口层级测试名称与断言不一致]**
>
> 证据：`VideoWorkflowNodeCard.spec.ts:103-110` 的用例名称为“keeps connection handles above the node body”，实际只断言节点 class 和端口数量，没有断言 z-index、pointer-events 或真实命中行为。
>
> 结果：删除本次关键 CSS 后测试仍会通过。
>
> 完整实现要求：增加浏览器级 pointer hit-test，或至少读取编译样式确认 handle 的 z-index/pointer-events。

当前相关单元测试实际覆盖：

- reverse handle 方向规范化；
- 图连接类型、单值端口和环校验；
- 节点库折叠事件；
- 节点卡片媒体预览和共享角色 handle；
- 画布基础事件与属性。

当前没有覆盖：重命名与自动保存并发、刷新后的 stale 清除、节点主体多同类输入选择、TouchEvent connect-end、Graph v2 时间线摘要。

## 13. 视频模型配置

`VideoGenWorkflowModels` 仍以 string 类型存储 JSON 数组。当前改动只修正文案，真实链路如下：

1. 后台设置页把多选结果序列化为 `{channel_type,value,label}[]` JSON。
2. handler 和 service 保存前都调用 `NormalizeVideoGenWorkflowModels()`。
3. 解析会 trim、校验渠道、按渠道和模型去重。
4. 读取失败或结果为空时回退到 `videogen.DefaultWorkflowModels()`。
5. server 启动时把读取函数注入视频工作流 handler。
6. 前端工作区通过 `/api/me/video-workflows/models` 获取可选模型。
7. 节点执行时根据模型值反查渠道，未命中则回退当前视频渠道。

因此设置项底层仍是 JSON 字符串；“保存时自动序列化”发生在后台页面的 computed setter，不是后端把任意多选对象自动转换。

> **[批注 P2｜后台允许清空多选，但保存后会静默恢复默认模型]**
>
> 证据：`Settings.vue:252-261` 将清空后的多选序列化为 `[]`，界面在 `952-985` 显示激活数量且没有“至少选择一项”的校验；`ParseVideoGenWorkflowModels()` 在 `internal/settings/service.go:637-639` 遇到过滤后空列表时返回默认模型，`NormalizeVideoGenWorkflowModels()` 随后把默认模型写回。
>
> 触发：管理员取消全部画布模型并保存。
>
> 结果：界面操作表达“0 个已激活”，实际持久化结果仍包含默认 Wan2.7 模型。
>
> 完整实现要求：明确产品约束后二选一：允许空列表并让业务读取尊重空列表；或前后端都强制至少选择一项并返回可见校验错误。

> **[批注 P3｜配置损坏时管理端显示与运行时回退不一致]**
>
> 证据：管理端 `parseWorkflowModels()` 在 `Settings.vue:657-669` 遇到非法 JSON 时返回空数组；运行时 `VideoGenWorkflowModels()` 在 `internal/settings/service.go:557-562` 遇到相同解析错误时返回默认模型。
>
> 触发：历史数据、人工改库或异常迁移导致 `system_settings.v` 不是合法 JSON。
>
> 结果：管理端显示 0 个激活模型，画布接口和执行侧实际继续使用默认模型，且没有错误标识。
>
> 完整实现要求：设置列表接口暴露解析错误，或管理端复用服务端规范化结果并显示配置损坏告警。

## 14. 网络测速脚本

### 14.1 参数和目标生成

脚本只解析 `--key value` 和无值布尔参数，重复参数转为数组。正整数参数非法时回退默认值：

- rounds=3；
- concurrency=4；
- timeout=30 秒；
- limit-assets=40。

目标 URL 来自 preset、重复 `--url` 和重复 `--page`，使用 Set 去重。`--page` 默认先用 curl 下载 HTML，再用 `src/href` 正则发现带常见静态资源扩展名的地址。

### 14.2 请求执行

- 启动时先执行 `curl --version`。
- 每个 URL 生成 rounds 个 job。
- `runPool()` 用共享递增索引控制固定并发 worker。
- curl 默认静默显示错误、压缩、跟随重定向，连接和总超时都使用 timeout。
- `-w` 输出最终 URL、HTTP 状态/版本、远端 IP、DNS/TCP/TLS/TTFB/total、大小、速度和重定向次数。
- 单请求失败被转换为结果对象，不中断其他 job。

### 14.3 汇总

结果按原始 URL 分组，计算 min、avg、p50、p95、max；p95 使用 nearest-rank，即 `ceil(n*0.95)-1`。最终按 total p95 从慢到快输出，可选写入 JSON。

> **[批注 P2｜“OK/退出码”和 HTTP 失败语义不一致]**
>
> 证据：`summarize()` 在 `scripts/net-speed-test.mjs:274-290` 把任何 `http_code>0` 且 curl 无错误的请求计入 ok，包括 4xx/5xx；`main()` 在 `90-91` 又只把 curl error 或 `http_code>=500` 计为失败。因此 404 被显示为 OK 且进程退出 0；`http_code=0` 但 curl 成功的协议会显示 0/N，进程仍退出 0。
>
> 实测：`file:///etc/hosts` 得到汇总 `OK 0/1`，脚本退出码为 0。
>
> 完整实现要求：统一成功谓词并在汇总、退出码、JSON 中复用；页面资源测速通常应明确采用 `200-399`，或提供可配置的成功状态范围。

### 14.4 已知能力边界

以下是代码明确限定的能力，不作为缺陷推测：

- HTML 资源发现不是 DOM/CSS 解析，只识别带指定扩展名的 `src/href`。
- 不模拟浏览器缓存、连接复用顺序、JavaScript 动态请求或渲染耗时。
- 并发 worker 的各次 curl 都是独立进程。
- `--insecure true` 会传递 curl `-k`，只应在明确接受证书校验缺失时使用。
- `--json` 直接调用 `writeFile`，不会递归创建用户指定的父目录。

> **[批注 P3｜资源发现阶段不检查页面 HTTP 状态]**
>
> 证据：`discoverAssets()` 在 `scripts/net-speed-test.mjs:127-154` 只根据 curl 进程是否报错决定是否解析 stdout；curl 未使用 `--fail`，也未读取 `%{http_code}`。
>
> 触发：`--page` 返回 401、404 或 500 HTML 错误页，且错误页包含带静态扩展名的 `src/href`。
>
> 结果：脚本会把错误页资源加入测速目标，汇总不能区分业务页面资源和错误页资源。
>
> 完整实现要求：资源发现请求读取最终 HTTP 状态，仅在明确允许的状态范围内解析 HTML，其他状态只保留页面本身的测速结果并输出 warning。

## 15. 验证结果

本次复盘实际执行：

- `go test ./internal/settings ./internal/videoworkflow`：通过。
- 相关 Vitest：4 个文件、44 项测试全部通过。
- `npm run build`：`vue-tsc --noEmit` 和 Vite 生产构建通过，转换 2041 个模块。
- 测速脚本无参数：正确输出帮助并以 1 退出。
- 测速脚本 file URL：复现“汇总 0/1 但退出 0”的失败语义不一致。

首次 Vitest 命令错误地在 `web` 工作目录下继续使用 `web/src/...` 路径，Vitest 报告未找到文件；改为 `src/...` 后 44 项全部通过。该次失败属于验证命令路径错误，不是代码失败。

## 16. 结论

已确认问题共 11 项：

- P1：2 项——重命名与自动保存竞态且吞错；刷新后的既有 stale 无法被成功运行清除。
- P2：6 项——前端漏校验 clip 连线；节点主体连接不优先空闲端口；触摸 connect-end 坐标未实现；Graph v2 时间线摘要读取旧字段；空模型选择被恢复为默认；测速脚本成功谓词不一致。
- P3：3 项——端口层级测试未验证标题所述行为；损坏模型配置的管理端与运行时表现不一致；测速资源发现不检查页面 HTTP 状态。

发布前至少应修复两项 P1；P2 中的连线和时间线展示直接影响当前功能正确性，建议与 P1 同批处理。
