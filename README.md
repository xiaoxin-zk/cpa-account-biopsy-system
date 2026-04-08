# CPA Account Biopsy System

Developer: Xiaoxin  
Version: 0.1-bate

## 中文说明

### 项目简介

CPA Account Biopsy System 是一个独立部署的账号活检 sidecar，专门为已运行的 CLIProxyAPI 提供账号健康检查、状态聚合、额度窗口展示、手动启停与独立 Web 仪表台。

它不会替换主项目，不会覆盖主项目原有安装方式，而是作为一个旁路 Docker 服务通过 management API 与主项目协作。

### 功能说明

- 展示账号池整体健康度
- 展示单账号状态：未探测、正常、额度/限流、401 封禁、已停用
- 展示额度窗口：周限额、代码审查周限额、5 小时限额
- 展示请求统计：请求数、成功数、失败数、Tokens
- 支持手动启用、停用、删除账号
- 支持独立 Web 密码
- 支持自动快照刷新和低频探测

### 页面说明

- 浏览器标题：`CPA账号活检系统`
- 页面主标题：`CPA账号活检系统`
- 页面显著位置展示：
  - `开发者 Xiaoxin`
  - `版本 0.1-bate`

### 依赖条件

- 已安装 Docker 与 Docker Compose
- 已存在可访问的 CLIProxyAPI 主服务
- 主服务已启用 management API
- 你已知道 management 密钥

### 快速开始

统一入口命令：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh)
```

运行后会出现菜单：

1. 安装
2. 更新
3. 卸载
4. 重启服务
5. 查看状态
6. 退出

脚本会自动尝试发现：

- CLIProxyAPI 容器
- auths 路径
- config.yaml 路径
- management 地址
- MANAGEMENT_PASSWORD 环境变量

如果某项自动发现失败，脚本只会针对缺失项交互询问。

### 配置项说明

- `CPA_MANAGEMENT_URL`: 主项目 management API 地址
- `CPA_MANAGEMENT_KEY`: 主项目 management 密钥
- `CPA_AUTH_DIR`: 主项目 auths 目录
- `CPA_CONFIG_PATH`: 主项目 config.yaml 路径
- `CPA_LISTEN_ADDR`: sidecar 监听地址，默认 `:18317`
- `CPA_SNAPSHOT_INTERVAL`: 快照刷新周期，默认 `5m`
- `CPA_PROBE_INTERVAL`: 低频探测周期，默认 `1h`
- `CPA_INSTALL_DIR`: 安装目录，默认 `/opt/cpa-account-biopsy-system`

### Docker 部署

项目自带：

- `Dockerfile`
- `docker-compose.yml`
- `.env.example`

进入安装目录后，也可以手工执行：

```bash
docker compose --env-file .env up -d --build
```

### 更新方式

再次执行同一条统一入口命令，然后在菜单中选择 `更新`。

也可以在安装目录中手动更新：

```bash
cd /opt/cpa-account-biopsy-system
git pull --ff-only
docker compose --env-file .env up -d --build
```

### 卸载方式

再次执行同一条统一入口命令，然后在菜单中选择 `卸载`。

### 与主项目对接方式

- sidecar 不替换主项目容器
- sidecar 通过 `CPA_MANAGEMENT_URL` 调用主项目 management API
- sidecar 通过 `CPA_AUTH_DIR` 读取账号文件
- sidecar 通过 `CPA_CONFIG_PATH` 读取主项目配置

### 常见问题

#### 1. 为什么它不会影响主项目？

因为它是独立 Docker 服务，不改主项目安装脚本，不替换主项目容器，也不改变主项目代理链路。

#### 2. 页面为什么先显示“未探测”？

sidecar 重启后，在本轮探测尚未执行前，会明确显示“未探测”，避免把历史状态误当成当前实时状态。

#### 3. 如何验证服务正常？

```bash
curl http://127.0.0.1:18317/healthz
```

### 安全说明

- 不要把真实账号文件、Token、管理密钥提交到公开仓库
- `.env` 默认不应提交
- sidecar 页面建议设置独立密码

---

## English

### Overview

CPA Account Biopsy System is a standalone sidecar service for CLIProxyAPI. It provides account health inspection, quota window visualization, status aggregation, manual enable/disable actions, and an independent Web dashboard.

It does not replace the main service. It runs as a separate Docker service and talks to CLIProxyAPI through the management API.

### Features

- Pool health overview
- Per-account state display: unprobed, active, quota-limited, blocked, disabled
- Quota window display: weekly quota, code review weekly quota, 5-hour quota
- Request statistics: requests, success, failures, tokens
- Manual enable / disable / delete actions
- Independent dashboard password
- Automatic snapshot refresh and low-frequency probing

### UI Branding

- Browser title: `CPA账号活检系统`
- Main title: `CPA账号活检系统`
- Visible credits:
  - `开发者 Xiaoxin`
  - `版本 0.1-bate`

### Requirements

- Docker and Docker Compose
- A running CLIProxyAPI service
- Management API enabled on the main service
- Management password available

### Quick Start

Unified entry command:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh)
```

You will get a terminal menu with:

1. Install
2. Update
3. Uninstall
4. Restart service
5. Show status
6. Exit

The installer will try to auto-detect:

- the CLIProxyAPI container
- the auths directory
- the config.yaml path
- the management URL
- the MANAGEMENT_PASSWORD runtime variable

If auto-discovery fails for a required field, the script will ask only for the missing value.

### Configuration

- `CPA_MANAGEMENT_URL`: CLIProxyAPI management API base URL
- `CPA_MANAGEMENT_KEY`: CLIProxyAPI management password
- `CPA_AUTH_DIR`: path to CLIProxyAPI auths directory
- `CPA_CONFIG_PATH`: path to CLIProxyAPI config.yaml
- `CPA_LISTEN_ADDR`: listen address, default `:18317`
- `CPA_SNAPSHOT_INTERVAL`: snapshot interval, default `5m`
- `CPA_PROBE_INTERVAL`: low-frequency probe interval, default `1h`
- `CPA_INSTALL_DIR`: install directory, default `/opt/cpa-account-biopsy-system`

### Docker Deployment

The project includes:

- `Dockerfile`
- `docker-compose.yml`
- `.env.example`

You can also run it manually after installation:

```bash
docker compose --env-file .env up -d --build
```

### Update

Run the same unified command again and choose `Update`.

Or update manually:

```bash
cd /opt/cpa-account-biopsy-system
git pull --ff-only
docker compose --env-file .env up -d --build
```

### Uninstall

Run the same unified command again and choose `Uninstall`.

### Integration with CLIProxyAPI

- The sidecar does not replace the main container
- It reads account data from `CPA_AUTH_DIR`
- It reads config from `CPA_CONFIG_PATH`
- It queries the main service via `CPA_MANAGEMENT_URL`

### FAQ

#### Why won’t it break the main service?

Because it runs as a separate Docker service and does not alter the main CLIProxyAPI installation flow.

#### Why do accounts show “unprobed” after restart?

Because the sidecar intentionally avoids treating historical states as current real-time states until a new probe cycle is completed.

#### How do I verify that it is healthy?

```bash
curl http://127.0.0.1:18317/healthz
```

### Security Notes

- Do not commit real auth files, tokens, or management keys
- `.env` should not be committed
- Use a dedicated dashboard password

---

Developer: Xiaoxin  
Version: 0.1-bate
#### 4. 首次访问为什么要求设置密码？

因为安装命令默认不再要求你把 Web 密码写在命令行里。首次打开页面时会进入初始化流程，在浏览器中设置仪表台密码，之后会自动持久化保存。

#### Why do I need to set a password on first visit?

Because the simplified installer no longer forces you to pass a dashboard password in the command line. The first visit opens a Web initialization flow where you create the dashboard password, and the system persists it automatically.
