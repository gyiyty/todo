# AstrBot 对接契约

## 任务 API

在设置页创建拥有 `tasks:read`、`tasks:write` 权限的 Token，以请求头调用：

```http
Authorization: Bearer tdk_xxx
```

插件可调用 `/api/v1/tasks`、`/api/v1/lists` 和 `/api/v1/tags`。API Token 不能访问账号、Token 或 Webhook 管理接口。

## 提醒 Webhook

插件使用 `context.register_web_api` 注册 POST 接口，再将完整地址填入设置页。请求包含：

```text
X-Todo-Event-ID: evt_xxx
X-Todo-Timestamp: 1784614800
X-Todo-Signature: sha256=<hex digest>
```

签名原文为 `timestamp + "." + 原始请求体`，算法为 HMAC-SHA256。插件应：

1. 拒绝与当前时间相差超过 5 分钟的请求。
2. 使用常量时间比较验证签名。
3. 按 `X-Todo-Event-ID` 去重。
4. 保存目标 QQ 会话的 `unified_msg_origin`。
5. 接收成功后尽快返回 2xx，再异步调用 `self.context.send_message`。

非 2xx 或连接失败会指数退避重试，最长保留 7 天；事件为至少一次投递。

`reminder.due` 请求体中的任务对象保持原字段兼容，并包含备注字段：

```json
{
  "task": {
    "id": "tsk_xxx",
    "title": "任务标题",
    "notes": "任务备注",
    "due_at": null,
    "priority": 0,
    "list": null
  }
}
```

`notes` 始终为字符串，未填写时是空字符串。新增该字段不改变签名原文、稳定 `event_id`、至少一次投递、去重或重试语义。

插件调用模型转述提醒时，只提供任务标题和非空备注，不提供截止时间、优先级、列表、任务 ID、提醒 ID 或 Webhook 元数据，也不携带聊天历史。模型的 `system_prompt` 只使用绑定会话最终生效的人格提示词。模型异常、超时或返回空内容时，插件仍发送原有固定格式提醒。

插件为六个 Todo LLM 工具提供独立开关，其中 `todo_create_task` 默认关闭，避免与 AstrBot 内置 `future_task` 重叠。`future_task` 仍由 AstrBot 自身的 `provider_settings.proactive_capability.add_cron_tools` 控制。
