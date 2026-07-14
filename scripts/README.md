# scripts

运维/自测辅助脚本集合。

> **⚠️ 注意** 本目录下所有 `--admin-email` / `--admin-pass` / `--user-email` / `--user-pass` 以及
> 形如 `admin@smoke.test`、`Admin123456`、`User123456` 这样的字面量,都是**冒烟脚本自己创建测试账号时用的临时凭证**,
> 它们 **不是** 部署后的默认管理员账号密码。
>
> GPT2API 本身不内置任何默认账号 —— **首位访问 `/register` 的用户自动成为 admin**(见根目录 README.md 的部署说明)。

## smoke.mjs · e2e 冒烟

对已启动的后端(本地 `go run` 或 `docker compose up -d`)做一轮端到端闭环自检。

### 覆盖用例


| #   | 用例                       | 说明                                                      |
| --- | ------------------------ | ------------------------------------------------------- |
| 1   | `/healthz`               | 后端可达                                                    |
| 2   | 首位用户自动 admin             | 若 users 表为空,register 的第一个账号自动拿到 admin 角色                |
| 3   | 普通用户注册 / 登录              |                                                         |
| 4   | `/api/me`、`/api/me/menu` | 断言 admin/user 各自的 role、permissions、menu 非空              |
| 5   | API Keys CRUD            | 用户视角 create / list / patch(禁用)/ delete                  |
| 6   | 越权校验                     | user token 访问 `/api/admin/*` 应 401/403;匿名访问 admin 应 401 |
| 7   | Admin 用户 / 分组列表          |                                                         |
| 8   | 调账 `+delta`              | 正确密码过,错误密码被拒(403),流水可查,用户余额同步                           |
| 9   | 审计日志                     | 含 `users.credit.adjust` 等动作                             |
| 10  | 备份链路                     | 创建 → 列表包含 → 下载 → 删除(二次密码);宿主缺 `mysqldump` 时跳过           |


### 用法

前置条件:Node ≥ 18(原生 fetch / FormData),后端已启动。

```bash
cd scripts
npm run smoke
```

或直接指定参数:

```bash
node scripts/smoke.mjs \
  --base http://localhost:8080 \
  --admin-email admin@smoke.test \
  --admin-pass  Admin123456 \
  --user-email  user@smoke.test \
  --user-pass   User123456
```

- `--keep true` 保留脚本创建的 Key、备份文件(便于后续手动验证)
- 环境变量 `GPT2API_BASE` 可覆盖 `--base`

### 退出码

- `0` 全部通过
- `1` 至少一条 FAIL
- `2` 脚本级异常(如 `/healthz` 不可达)

### 复跑行为

脚本是幂等的——已经存在的账号走登录路径,已经存在的 key 不影响新建。但它假设 "首位用户 = admin" 那步只在空库时成立,所以:

- 对全新库:能完整跑通
- 对已经跑过的库:要么复用相同 admin 账号(`--admin-email` 指向那个),要么清空 users 表再跑

### 与 CI 配合

GitHub Actions 示例骨架:

```yaml
- name: docker compose up
  run: docker compose -f deploy/docker-compose.yml up -d --wait

- name: wait backend
  run: curl --retry 30 --retry-delay 2 --retry-connrefused http://localhost:8080/healthz

- name: smoke
  run: node scripts/smoke.mjs --base http://localhost:8080
```

## local-gen-mock · 本地文本/生图/生视频上游

不依赖真模型，用占位 PNG + 15s MP4 跑通视频工作流（含 compose）。

```bash
# 启动 mock，并写入 system_settings；同时重启本机后端
bash scripts/local-gen-up.sh --with-backend

# 仅 mock（后端已在跑时，改完 settings 后在后台点「重载」或重启）
bash scripts/local-gen-up.sh

# 端到端冒烟（需 admin + self:video_workflow）
node scripts/local-workflow-smoke.mjs \
  --base http://127.0.0.1:8080 \
  --email you@example.com \
  --pass 'YourPass'
```

Mock 监听 `127.0.0.1:8790`，协议对齐：

- **gpt-image-2**：`POST /v1/images/generations`、`/v1/images/edits`
- **Seedance 2.0**：`POST/GET /seedance/api/v3/contents/generations/tasks`
- 文本：`POST /v1/chat/completions`

开发环境下载 mock 视频需 `GPT2API_APP_ENV=dev` 或 `GPT2API_VIDEO_WORKFLOW_ALLOW_LOCAL_MEDIA=1`（放行 `http://127.0.0.1`）。

停止：`bash scripts/local-gen-up.sh --stop`

## net-speed-test.mjs · 网络/页面资源测速

用于排查页面卡顿到底慢在 DNS、TCP 连接、TLS、首包、下载吞吐，还是某个静态资源/接口本身。

脚本底层调用 `curl`，输出每次请求的:

- HTTP 状态与版本
- DNS / connect / TLS / TTFB / total
- 下载体积与下载速度
- 汇总 avg / p95

### 用法

```bash
node scripts/net-speed-test.mjs \
  --page https://ai.reeko.net.cn:8081/admin/ops \
  --rounds 3 \
  --concurrency 4
```

内置 Sub2API 新旧机对比:

```bash
node scripts/net-speed-test.mjs --preset sub2api --rounds 5
```

输出 JSON 便于留档:

```bash
node scripts/net-speed-test.mjs \
  --page https://ai.reeko.net.cn:8081/admin/ops \
  --rounds 5 \
  --json output/sub2api-speed.json
```

常用参数:

- `--url URL`: 单独指定测速 URL，可重复。
- `--page URL`: 拉取页面并自动发现 `src/href` 里的 JS/CSS/图片资源。
- `--http auto|1.1|2`: 指定 HTTP 协议。
- `--compressed false`: 关闭 gzip/br 请求。
- `--insecure true`: 跳过证书校验。
- `--assets false`: 只测页面 HTML，不自动测静态资源。
