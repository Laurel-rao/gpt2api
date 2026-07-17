# AGENTS.md

## 语言

- 默认使用中文回复。

## 默认部署方式

- 日常把当前工作区改动发布到远端/测试环境时,默认使用:

```bash
bash deploy/quick-remote-sync.sh [相关文件...]
```

- 不要优先使用 `deploy/remote-release.sh`、手工打 tar 包、手工 scp/rsync 整包发布。`remote-release.sh` 只在明确需要完整远端应用/数据库备份、备份列表、自动回滚或正式 release 包时使用。
- `quick-remote-sync.sh` 默认读取 `deploy/remote-release.env` 中的远端配置,会执行本地前端构建、本地 Go 测试、同步源码和 `web/dist`、远端 Go 测试、远端静态编译、Docker compose 重建和 `/healthz` 检查。
- 如果改动包含新增文件、SQL 迁移、路由、菜单、部署配置或前端新页面,必须把这些完整相关文件追加到 quick 脚本参数里。不要只传最后修改的一个文件,避免远端源码不完整导致构建回退或功能缺失。
- 只有在已经确认刚跑过本地构建和测试时,才可以给 quick 脚本加 `--skip-local-build`。
- quick 脚本最后的 `curl` 可能撞上 nginx 重启瞬间而出现一次 connection reset。遇到这种情况,补跑:

```bash
curl -kfsS https://64.83.17.240:8000/healthz
ssh root@64.83.17.240 "cd /opt/gpt2api/deploy && docker compose ps"
```

只要外部 `/healthz` 正常且 `server/nginx` 运行健康,部署视为成功。
