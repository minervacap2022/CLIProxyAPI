---
name: cliproxyapi-config
description: Configure CLIProxyAPI model groups, API key policies, and failover routing via the management REST API. Use this skill when a user wants to set up model groups, bind API keys to groups, configure priority-based failover, or manage access policies on their CLIProxyAPI proxy server.
origin: local
---

# CLIProxyAPI Configuration Skill

This skill teaches you how to configure a running CLIProxyAPI instance: creating model groups for priority-based failover routing, and binding API keys to access policies.

## When to Activate

- User wants to add or update a model group
- User wants to restrict an API key to certain models
- User wants to set up automatic failover (e.g., Claude → Doubao when quota exhausted)
- User wants to configure load balancing across multiple credentials
- User asks "how do I configure my proxy" or "set up failover"

---

## Platform Overview

CLIProxyAPI is a self-hosted LLM proxy that:
- Accepts standard OpenAI-compatible API calls from clients
- Routes requests to upstream providers (Claude, Gemini, Doubao, OpenAI, etc.)
- Manages multiple auth credentials per provider
- Supports **model groups**: virtual model names that resolve to real models with priority-based failover

### Key Concepts

| Concept | Description |
|---------|-------------|
| **Model Group** | A virtual model name (e.g. `"auto"`) that maps to real models by priority |
| **Priority Tier** | Models at the same priority level — tried round-robin (load balanced) |
| **Failover** | When all models in the highest-priority tier fail, the next tier is tried |
| **API Key Config** | Policy bound to a client API key: which group it can use, whether other models are allowed |
| **allow-other-models** | If `true`, the key can use any model directly; if `false`, only the group name works |

### Failover Trigger Conditions

The proxy falls through to the next priority tier when upstream returns:
- `429` Too Many Requests (quota exhausted)
- `402` Payment Required
- `401` Unauthorized (credential invalid/expired)
- `403` Forbidden
- `500` / `502` / `503` / `504` (server errors)
- `auth_not_found` (all credentials for that model are exhausted)

`400 Bad Request` does **not** trigger failover (request itself is malformed).

---

## Authentication

All management API calls require the server's secret key:

```bash
# Via Authorization header (preferred)
-H "Authorization: Bearer <management-secret>"

# Or via custom header
-H "X-Management-Key: <management-secret>"
```

The secret is set in the server config under `remote-management.secret-key`.

---

## Model Groups API

Base path: `POST/GET/PATCH/DELETE /v0/management/model-groups`

### List all groups
```bash
curl http://localhost:8318/v0/management/model-groups \
  -H "Authorization: Bearer <secret>"
```

**Response:**
```json
{
  "model-groups": [
    {
      "name": "auto",
      "models": [
        {"model": "claude-sonnet-4-6", "priority": 2},
        {"model": "claude-haiku-4-5", "priority": 2},
        {"model": "doubao-pro-32k", "priority": 1}
      ]
    }
  ]
}
```

### Create or update a group (upsert by name)
```bash
curl -X PATCH http://localhost:8318/v0/management/model-groups \
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

### Delete a group
```bash
curl -X DELETE "http://localhost:8318/v0/management/model-groups?name=auto" \
  -H "Authorization: Bearer <secret>"
```

### Replace all groups
```bash
curl -X PUT http://localhost:8318/v0/management/model-groups \
  -H "Authorization: Bearer <secret>" \
  -H "Content-Type: application/json" \
  -d '{"model-groups": [...]}'
```

### Priority Rules

```
Priority 2: claude-sonnet-4-6, claude-haiku-4-5   ← tried first, round-robin between them
     ↓ failover (when both return 429/401/5xx)
Priority 1: doubao-pro-32k                         ← fallback
```

- **Same priority** = all models in that tier are load-balanced (round-robin)
- **Higher number** = higher priority (tried first)
- **Lower tier** = automatic failover destination

---

## API Key Configs API

Base path: `GET/PATCH/DELETE /v0/management/api-key-configs`

### List all key configs
```bash
curl http://localhost:8318/v0/management/api-key-configs \
  -H "Authorization: Bearer <secret>"
```

**Response:**
```json
{
  "api-key-configs": [
    {
      "key": "sk-my-agent",
      "label": "My Agent",
      "model-group": "auto",
      "allow-other-models": false
    }
  ]
}
```

### Create or update a key config (upsert by key)
```bash
curl -X PATCH http://localhost:8318/v0/management/api-key-configs \
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

| Field | Type | Description |
|-------|------|-------------|
| `key` | string | **Required.** The API key the client will use |
| `label` | string | Human-readable name for this key |
| `model-group` | string | Group name to bind; client must request this as the `model` field |
| `allow-other-models` | bool | `true` = key can use any model directly; `false` = only the group name |

### Delete a key config
```bash
curl -X DELETE "http://localhost:8318/v0/management/api-key-configs?key=sk-my-agent" \
  -H "Authorization: Bearer <secret>"
```

---

## Common Configuration Recipes

### Recipe 1: Claude primary + Doubao fallback

```bash
SECRET="your-secret"
BASE="http://localhost:8318/v0/management"

# Step 1: Create group
curl -X PATCH $BASE/model-groups \
  -H "Authorization: Bearer $SECRET" -H "Content-Type: application/json" \
  -d '{"value": {"name": "claude-auto", "models": [
    {"model": "claude-sonnet-4-6", "priority": 2},
    {"model": "doubao-pro-32k",    "priority": 1}
  ]}}'

# Step 2: Create key config
curl -X PATCH $BASE/api-key-configs \
  -H "Authorization: Bearer $SECRET" -H "Content-Type: application/json" \
  -d '{"value": {"key": "sk-client-001", "label": "Client", "model-group": "claude-auto"}}'

# Step 3: Client calls the proxy using group name as model
curl http://localhost:8318/v1/chat/completions \
  -H "Authorization: Bearer sk-client-001" \
  -d '{"model": "claude-auto", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Recipe 2: Load-balanced multi-account Claude

```bash
# Two Claude accounts at same priority → round-robin
curl -X PATCH $BASE/model-groups \
  -H "Authorization: Bearer $SECRET" -H "Content-Type: application/json" \
  -d '{"value": {"name": "claude-lb", "models": [
    {"model": "claude-sonnet-4-6", "priority": 2},
    {"model": "claude-sonnet-4-6", "priority": 2}
  ]}}'
```

> Note: credential selection within a model is handled by the auth pool — if you have 2 Claude auth files, they will be round-robined automatically.

### Recipe 3: Unrestricted key (admin/debug)

```bash
curl -X PATCH $BASE/api-key-configs \
  -H "Authorization: Bearer $SECRET" -H "Content-Type: application/json" \
  -d '{"value": {"key": "sk-admin", "label": "Admin", "model-group": "claude-auto", "allow-other-models": true}}'
# allow-other-models: true → can call any model directly, not just group name
```

---

## Asking Users the Right Questions

When a user wants to configure failover, ask:

1. **Which models should be primary?** (e.g., Claude Sonnet)
2. **Which models should be fallback?** (e.g., Doubao Pro)
3. **Should primary models be load-balanced?** (if yes → same priority number)
4. **What API key will the client use?** (create key config with that value)
5. **Should that key be restricted to only this group?** (`allow-other-models: false`) or can it call any model? (`true`)

Then execute the PATCH calls above in order: group first, then key config.

---

## Important Notes

- Changes take effect **immediately** — no server restart needed
- Config is persisted to disk automatically after each successful write
- The group `name` field becomes the `model` value clients send in API requests
- A key without any `api-key-configs` entry has **no model restrictions** (backward compatible)
- The management API base path is `/v0/management/` (not `/v1/`)
