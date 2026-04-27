# CLIProxyAPI — Agent Configuration Guide

This document is written for AI agents. It covers everything needed to deploy and configure a CLIProxyAPI instance programmatically.

---

## What is CLIProxyAPI?

A self-hosted LLM proxy that:
- Accepts OpenAI-compatible API calls from clients (`/v1/chat/completions`, `/v1/models`, etc.)
- Routes requests to upstream providers: Claude (OAuth), Gemini, Doubao, OpenAI-compatible APIs, etc.
- Manages multiple auth credentials per provider with automatic round-robin load balancing
- Supports **model groups**: virtual model names with priority-based failover across providers

---

## Deployment

### Minimum viable start

```bash
docker run -d \
  --name cliproxyapi \
  -p 8317:8317 \
  -e MANAGEMENT_PASSWORD=your-secret \
  -v $(pwd)/config.yaml:/CLIProxyAPI/config.yaml \
  -v $(pwd)/auths:/root/.cli-proxy-api \
  ghcr.io/minervacap2022/cliproxyapi:latest
```

- `MANAGEMENT_PASSWORD` enables the management API with remote access — no config file needed to start
- On first start the container creates missing directories and seeds `config.yaml` from the built-in template if it is empty
- `config.yaml` is written by the management API when settings are saved; mount it for persistence
- `auths/` holds provider credential files (Claude OAuth tokens, etc.)

### Docker Compose

```bash
MANAGEMENT_PASSWORD=your-secret docker compose up -d
```

---

## Management API

Base URL: `http://localhost:8317/v0/management`

### Authentication

```
Authorization: Bearer <MANAGEMENT_PASSWORD>
```

or

```
X-Management-Key: <MANAGEMENT_PASSWORD>
```

All write operations persist to `config.yaml` automatically.

---

## Model Groups

Model groups are virtual model names. When a client sends `"model": "auto"`, the proxy resolves it to real models by priority tier.

**Priority rules:**
- Same priority number → load-balanced (round-robin)
- Higher priority number → tried first
- Lower priority tier → automatic failover when higher tier returns 429 / 401 / 403 / 5xx

**Failover triggers:** `429`, `402`, `401`, `403`, `500`, `502`, `503`, `504`, `auth_not_found`
**No failover:** `400` (bad request — the request itself is malformed)

### List groups

```bash
curl http://localhost:8317/v0/management/model-groups \
  -H "Authorization: Bearer <secret>"
```

```json
{
  "model-groups": [
    {
      "name": "auto",
      "models": [
        {"model": "claude-sonnet-4-6", "priority": 2},
        {"model": "doubao-pro-32k",    "priority": 1}
      ]
    }
  ]
}
```

### Create or update (upsert by name)

```bash
curl -X PATCH http://localhost:8317/v0/management/model-groups \
  -H "Authorization: Bearer <secret>" \
  -H "Content-Type: application/json" \
  -d '{
    "value": {
      "name": "auto",
      "models": [
        {"model": "claude-sonnet-4-6", "priority": 2},
        {"model": "claude-haiku-4-5",  "priority": 2},
        {"model": "doubao-pro-32k",    "priority": 1}
      ]
    }
  }'
```

### Delete

```bash
curl -X DELETE "http://localhost:8317/v0/management/model-groups?name=auto" \
  -H "Authorization: Bearer <secret>"
```

### Replace all

```bash
curl -X PUT http://localhost:8317/v0/management/model-groups \
  -H "Authorization: Bearer <secret>" \
  -H "Content-Type: application/json" \
  -d '{"model-groups": [...]}'
```

---

## API Key Configs

Bind a client API key to a model group and access policy.

| Field | Type | Description |
|-------|------|-------------|
| `key` | string | **Required.** Client Bearer token value |
| `label` | string | Human-readable name |
| `model-group` | string | Group name the client must request as `model` |
| `allow-other-models` | bool | `true` = key can bypass group and use any model directly |

### List

```bash
curl http://localhost:8317/v0/management/api-key-configs \
  -H "Authorization: Bearer <secret>"
```

### Create or update (upsert by key)

```bash
curl -X PATCH http://localhost:8317/v0/management/api-key-configs \
  -H "Authorization: Bearer <secret>" \
  -H "Content-Type: application/json" \
  -d '{
    "value": {
      "key": "sk-my-agent",
      "label": "My Agent",
      "model-group": "auto",
      "allow-other-models": false
    }
  }'
```

### Delete

```bash
curl -X DELETE "http://localhost:8317/v0/management/api-key-configs?key=sk-my-agent" \
  -H "Authorization: Bearer <secret>"
```

---

## Client API Keys (flat list)

Simple API keys without group restrictions:

```bash
# Add a key
curl -X PATCH http://localhost:8317/v0/management/api-keys \
  -H "Authorization: Bearer <secret>" \
  -H "Content-Type: application/json" \
  -d '{"value": "sk-new-key"}'

# List all keys
curl http://localhost:8317/v0/management/api-keys \
  -H "Authorization: Bearer <secret>"

# Delete
curl -X DELETE "http://localhost:8317/v0/management/api-keys?key=sk-old-key" \
  -H "Authorization: Bearer <secret>"
```

---

## Complete Setup Recipe

```bash
SECRET="your-management-secret"
BASE="http://localhost:8317/v0/management"

# 1. Create model group: Claude primary, Doubao fallback
curl -X PATCH $BASE/model-groups \
  -H "Authorization: Bearer $SECRET" -H "Content-Type: application/json" \
  -d '{"value": {"name": "auto", "models": [
    {"model": "claude-sonnet-4-6", "priority": 2},
    {"model": "doubao-pro-32k",    "priority": 1}
  ]}}'

# 2. Create API key bound to the group
curl -X PATCH $BASE/api-key-configs \
  -H "Authorization: Bearer $SECRET" -H "Content-Type: application/json" \
  -d '{"value": {"key": "sk-client", "label": "Client", "model-group": "auto"}}'

# 3. Client calls proxy using group name as model
curl http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer sk-client" \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Hello"}]}'
```

---

## Skill

A Claude Code skill for agent-assisted configuration is at:

```
skills/cliproxyapi-config/SKILL.md
```

Copy it to `~/.claude/skills/cliproxyapi-config/` to enable in any Claude Code session.

---

## Notes

- All management writes take effect immediately — no server restart needed
- A key in `api-keys` without an `api-key-configs` entry has no model restrictions (backward compatible)
- The management panel (web UI) is at `http://your-server:8317/management.html`
- Default port is `8317`; override with `port:` in config or `-p HOST_PORT:8317` in Docker
