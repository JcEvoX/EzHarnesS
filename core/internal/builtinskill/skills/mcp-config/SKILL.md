---
name: mcp-config
description: 给应用增删改 MCP 服务器——直接编辑数据目录的 mcp.json，改动下一轮迭代自动热加载，无需重启
---

# MCP 服务器自配置

`<数据目录>/mcp.json`（数据目录绝对路径见 system 提示 `<workspace>` 段第一行）是 MCP 服务器的唯一配置来源。你可以直接编辑它：**保存后的下一轮迭代自动热加载**（连接重建），新工具面经 `<resource_change>` 反馈，无需重启应用。

## 配置文件结构

```json
{
  "servers": [
    {
      "name": "唯一标识",
      "description": "用途说明（给模型看，必写清楚）",
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer <key>" },
      "allow": ["tool_a", "tool_b"],
      "enabled": true
    }
  ]
}
```

字段规则：
- `type`：`http` | `sse` | `stdio`
- `http`/`sse` 型给 `url`（+可选 `headers` 鉴权）；`stdio` 型给 `command` + `args`（+可选 `env`）
- `allow` 是工具白名单，省略 = 全部允许；已有条目的 `allow` 必须原样保留
- `enabled` 省略 = 启用；禁用条目保留配置但不装配
- API key 放 `headers`/`env`，不要在对用户的回复里明文复述

## OAuth 认证（自动，通常无需配置）

**OAuth 是自动检测的**——只要服务是 http/sse 且用 OAuth，接入后无需任何配置：某次调用遇到 OAuth 需求（401 通告了 RFC9728 资源元数据）即自动进入认证流程。你**不需要**在 `mcp.json` 里加 `oauth` 块。

只有当服务**不支持下自动注册（DCR）**、或要**指定 scope / 手工发现地址**时，才加 `oauth` 块做覆盖：

```json
{
  "name": "notion",
  "type": "http",
  "url": "https://mcp.example.com/v1/mcp",
  "oauth": { "enabled": true, "client_id": "…", "scopes": ["read"], "metadata_url": "…" }
}
```

- `oauth.enabled`：显式强制走 OAuth（一般不用——自动检测已覆盖）
- `oauth.client_id` / `oauth.client_secret`：服务不支持 DCR 时才填，否则留空
- `oauth.scopes`：可选，申请的权限范围数组
- `oauth.metadata_url`：可选，授权服务器元数据地址（自动发现失败时手工指定）

**首次需要认证时**会在会话里出现一张「MCP 需要授权」决策卡（和审批卡一样内联在时间线）：点「去授权」→ 系统浏览器完成授权 → 该次调用自动重试。用户也可在 MCP 设置页点「认证」，或在通知栏处理。令牌持久化在数据目录 `mcp-auth/` 下，重启仍有效。用 OAuth 的服务不要再在 `headers` 里写 `Authorization`。

## 操作流程

1. `read_file` 读 `<数据目录>/mcp.json`（绝对路径，从 `<workspace>` 段的数据目录行拼）
2. 解析后按需追加/修改/删除条目，**全量保留其余条目与所有字段**
3. `write_file` 整体写回（合法 JSON、缩进 2 空格）
4. 告知用户已配置；新工具下轮迭代即可用，无需其他操作

无法确定服务地址或鉴权方式时，先问用户，不要猜着配。
