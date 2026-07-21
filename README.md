# 拾光待办

面向个人使用的轻量待办服务：Go 单体、Preact PWA、SQLite、AstrBot API/Webhook 预留。

开发者和后续 agent 请先阅读 [`AGENTS.md`](AGENTS.md) 与 [`docs/development.md`](docs/development.md)。AstrBot 协议单独记录在 [`docs/astrbot-integration.md`](docs/astrbot-integration.md)。

## 本机构建与运行

```bash
make build
printf '%s\n' '至少10位的密码' | ./bin/todo admin create yourname
./bin/todo
```

默认仅监听 `127.0.0.1:8787`。开发或临时访问可用 SSH 隧道：

```bash
ssh -L 8787:127.0.0.1:8787 user@server
```

然后访问 `http://127.0.0.1:8787`。

## Docker 部署

```bash
cp .env.example .env
# 修改 TODO_BASE_URL 为实际 HTTPS 域名
docker compose up -d --build
printf '%s\n' '管理员密码' | docker compose exec -T todo todo admin create yourname
docker compose exec todo todo backup
```

容器只向宿主机回环地址映射端口。用 `deploy/nginx.conf.example` 配置宝塔反向代理并申请 HTTPS 证书。

## 运维

- 健康检查：`GET /health/live`、`GET /health/ready`
- 手动备份：`./bin/todo backup`
- 重置密码：`printf '%s\n' '新密码' | ./bin/todo admin reset-password yourname`
- 每次备份后自动删除超过 14 天的 `todo-*.db`
- 服务会在每天 03:20 后自动创建一次备份；systemd timer 仅作为额外保障
- AstrBot 对接见 `docs/astrbot-integration.md`

生产环境必须将 `TODO_BASE_URL` 设置为最终 HTTPS 地址，否则登录 Cookie 不会启用 Secure 属性。

本机 systemd 用户服务可在普通 SSH 会话中启用：

```bash
systemctl --user link "$PWD/deploy/systemd/todo.service" "$PWD/deploy/systemd/todo-backup.service" "$PWD/deploy/systemd/todo-backup.timer"
systemctl --user daemon-reload
systemctl --user enable --now todo.service todo-backup.timer
```
