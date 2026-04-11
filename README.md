# CPA Account Biopsy System

Developer: Xiaoxin  
Version: 0.2-bate

## 中文说明

### 项目简介

CPA Account Biopsy System 是一个独立部署的账号活检 sidecar，专门为已运行的 CLIProxyAPI 提供账号健康检查、状态聚合、额度窗口展示、手动启停与独立 Web 仪表台。

它不会替换主项目，不会覆盖主项目原有安装方式，而是作为一个旁路 Docker 服务通过 management API 与主项目协作。

### 友情链接

- [Linux.do](https://linux.do/)

### 功能说明

- 展示账号池整体健康度
- 展示单账号状态：未探测、正常、额度/限流、401 封禁、已停用
- 展示额度窗口：周限额、代码审查周限额、5 小时限额
- 展示请求统计：请求数、成功数、失败数、Tokens
- 支持手动启用、停用、删除账号
- 支持独立 Web 密码
- 支持自动快照刷新和低频探测

### 当前状态

- 当前仓库已可独立部署、更新和卸载
- 当前重点仍然是提升服务器探测可信度、额度状态判定和可维护性
- 欢迎社区参与修复、测试、文档整理和体验改进

### 参与共建

欢迎所有开发者通过 Issue 和 Pull Request 一起参与共建这个项目。

- 贡献说明：[`CONTRIBUTING.md`](./CONTRIBUTING.md)
- 行为准则：[`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md)
- 安全问题：[`SECURITY.md`](./SECURITY.md)
- 项目路线图：[`ROADMAP.md`](./ROADMAP.md)

如果你是第一次参与，建议优先从文档、测试、错误提示、前端状态说明和脚本健壮性改进开始。

### 页面说明

- 浏览器标题：`CPA账号活检系统`
- 页面主标题：`CPA账号活检系统`
- 页面显著位置展示：
  - `开发者 Xiaoxin`
  - `版本 0.2-bate`

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

### Windows 安装说明

- Windows 下不需要手动填写 `C:\...` 或 `D:\...` 绝对路径作为安装前提。
- `scripts/manage.sh` 本身是 Bash 脚本，因此 Windows 下需要 **Git Bash** 或 **WSL** 才能真正执行安装脚本。
- 安装脚本会优先自动发现可用账号目录。
- 如果未发现可用目录，会自动回退到安装目录下的数据目录。
- Windows 默认安装目录为：`%USERPROFILE%\cpa-account-biopsy-system`
- Windows 默认账号目录为：`%USERPROFILE%\cpa-account-biopsy-system\data\auths`
- Windows Docker 部署完成后，默认本机访问地址为：`http://localhost:18317/`
- 只有高级场景下，才建议手动指定自定义账号目录。

推荐方式：

- 默认主流程：**Windows PowerShell 启动 Git Bash 包装命令**
- 直接执行方式：**Git Bash**
- 高级方式：**WSL** 或自定义目录安装

如果你输入了无效目录，脚本会：

- 明确提示目录无效
- 自动回退到默认目录
- 告知最终实际使用的目录

#### Windows PowerShell 默认安装 / 启动流程

说明：

- 这是当前推荐的 Windows 默认主流程。
- 它会在 PowerShell 中下载脚本，并自动探测本机可用的 Bash 来执行。
- 不要再把 Unix 的 `bash <(curl ...)` 当成 Windows 用户默认安装方式。

```powershell
$script = Join-Path $env:TEMP 'cpa-manage.sh'
Invoke-WebRequest 'https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh' -OutFile $script
$bash = $null
$cmd = Get-Command bash -ErrorAction SilentlyContinue
if ($cmd) {
  $bash = $cmd.Source
}
if (-not $bash) {
  $candidates = @(
    'C:\Program Files\Git\bin\bash.exe',
    'C:\Program Files\Git\usr\bin\bash.exe',
    'C:\Program Files (x86)\Git\bin\bash.exe',
    'C:\Program Files (x86)\Git\usr\bin\bash.exe'
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      $bash = $candidate
      break
    }
  }
}
if (-not $bash) {
  Write-Error '未找到可用的 bash。请先安装 Git for Windows（Git Bash），或改用 WSL 后再执行安装脚本。'
  return
}
& $bash $script
```

如果系统里已经有 `bash` 命令，PowerShell 会优先直接使用；只有找不到时才会继续探测常见 Git Bash 安装路径。

#### Git Bash 默认安装 / 启动流程

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh)
```

说明：

- 仅适用于 Git Bash / WSL 终端
- 不适合作为普通 Windows PowerShell 用户的默认说明
- 找不到主项目账号目录时，会自动回退到 `%USERPROFILE%\cpa-account-biopsy-system\data\auths`
- 部署完成后，直接在浏览器打开 `http://localhost:18317/`

#### Windows 自定义目录流程（PowerShell）

```powershell
$env:CPA_AUTH_DIR = 'D:/CPAData/auths'
$script = Join-Path $env:TEMP 'cpa-manage.sh'
Invoke-WebRequest 'https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh' -OutFile $script
$bash = $null
$cmd = Get-Command bash -ErrorAction SilentlyContinue
if ($cmd) {
  $bash = $cmd.Source
}
if (-not $bash) {
  $candidates = @(
    'C:\Program Files\Git\bin\bash.exe',
    'C:\Program Files\Git\usr\bin\bash.exe',
    'C:\Program Files (x86)\Git\bin\bash.exe',
    'C:\Program Files (x86)\Git\usr\bin\bash.exe'
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      $bash = $candidate
      break
    }
  }
}
if (-not $bash) {
  Write-Error '未找到可用的 bash。请先安装 Git for Windows（Git Bash），或改用 WSL 后再执行安装脚本。'
  return
}
& $bash $script
```

#### Windows 自定义目录流程（Git Bash）

```bash
export CPA_AUTH_DIR='D:/CPAData/auths'
bash <(curl -fsSL https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh)
```

说明：

- 仅高级场景才建议这样做
- 自定义目录会先校验和规范化
- 如果目录无效，脚本不会直接生成错误 volume，而是自动回退到默认目录

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

### 目录结构

- `cmd/cpa-account-biopsy-system/`: 程序入口
- `internal/accounthealth/`: 核心后端逻辑与内嵌页面
- `scripts/manage.sh`: 安装、更新、卸载、重启统一入口
- `docker-compose.yml`: 本地/服务器部署编排
- `README.md`: 项目说明

### 更新方式

再次执行同一条统一入口命令，然后在菜单中选择 `更新`。

也可以在安装目录中手动更新：

```bash
cd /opt/cpa-account-biopsy-system
git pull --ff-only
docker compose --env-file .env up -d --build
```

Windows 下如果使用默认安装目录，可在对应安装目录中执行相同的更新逻辑；不需要手填新的绝对路径。

### 卸载方式

再次执行同一条统一入口命令，然后在菜单中选择 `卸载`。

### 与主项目对接方式

- sidecar 不替换主项目容器
- sidecar 通过 `CPA_MANAGEMENT_URL` 调用主项目 management API
- sidecar 通过 `CPA_AUTH_DIR` 读取账号文件
- sidecar 不再依赖宿主机 config.yaml 挂载，核心依赖是 management API 与 auths 目录

### 常见问题

#### 1. 为什么它不会影响主项目？

因为它是独立 Docker 服务，不改主项目安装脚本，不替换主项目容器，也不改变主项目代理链路。

#### 2. 页面为什么先显示“未探测”？

sidecar 重启后，在本轮探测尚未执行前，会明确显示“未探测”，避免把历史状态误当成当前实时状态。

#### 3. 如何验证服务正常？

```bash
curl http://127.0.0.1:18317/healthz
```

Windows Docker 默认访问面板：

```text
http://localhost:18317/
```

#### 4. 首次访问为什么要求设置密码？

因为安装命令默认不再要求你把 Web 密码写在命令行里。首次打开页面时会进入初始化流程，在浏览器中设置仪表台密码，之后会自动持久化保存。

#### 5. 为什么修改密码后会被强制退出？

这是预期的安全行为。只要 Web 管理密码被修改，当前登录态会立即失效，系统会强制退出并要求你使用新密码重新登录；旧密码和旧登录态会同时失效。

### 安全说明

- 不要把真实账号文件、Token、管理密钥提交到公开仓库
- `.env` 默认不应提交
- sidecar 页面建议设置独立密码

### 协作入口

- 报告 Bug：使用 GitHub Bug Report 模板
- 提功能建议：使用 GitHub Feature Request 模板
- 提交代码：Fork 仓库并发起 Pull Request
- 寻找可参与任务：查看 `ROADMAP.md` 中的 `Good First Issue` 和 `Help Wanted`

### 开源协议

本项目当前仓库附带 `MIT` 协议文件，参与贡献前请先阅读仓库中的 [`LICENSE`](./LICENSE)。

---

## English

### Overview

CPA Account Biopsy System is a standalone sidecar service for CLIProxyAPI. It provides account health inspection, quota window visualization, status aggregation, manual enable/disable actions, and an independent Web dashboard.

It does not replace the main service. It runs as a separate Docker service and talks to CLIProxyAPI through the management API.

### Friend Link

- [Linux.do](https://linux.do/)

### Features

- Pool health overview
- Per-account state display: unprobed, active, quota-limited, blocked, disabled
- Quota window display: weekly quota, code review weekly quota, 5-hour quota
- Request statistics: requests, success, failures, tokens
- Manual enable / disable / delete actions
- Independent dashboard password
- Automatic snapshot refresh and low-frequency probing

### Project Status

- The project is deployable today as a standalone sidecar
- Current work is focused on probe correctness, quota-state handling, and safer maintenance flows
- Community contributions are welcome for code, tests, docs, UI clarity, and deployment experience

### Contributing

Everyone is welcome to help build this project.

- Contribution guide: [`CONTRIBUTING.md`](./CONTRIBUTING.md)
- Code of conduct: [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md)
- Security reporting: [`SECURITY.md`](./SECURITY.md)
- Roadmap: [`ROADMAP.md`](./ROADMAP.md)

If you are looking for a first contribution, docs, tests, issue reproduction, and UX clarity improvements are great starting points.

### UI Branding

- Browser title: `CPA账号活检系统`
- Main title: `CPA账号活检系统`
- Visible credits:
  - `开发者 Xiaoxin`
  - `版本 0.2-bate`

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

### Windows Notes

- Windows users do not need to manually enter a `C:\...` or `D:\...` absolute path as the normal installation flow.
- `scripts/manage.sh` is a Bash installer, so Windows needs **Git Bash** or **WSL** to actually run it.
- The installer first tries auto-discovery.
- If no valid auth directory is found, it falls back to the install-local data directory.
- Default Windows install directory: `%USERPROFILE%\cpa-account-biopsy-system`
- Default Windows auth directory: `%USERPROFILE%\cpa-account-biopsy-system\data\auths`
- Default Windows dashboard address after Docker deployment: `http://localhost:18317/`
- Manual custom directories are intended only for advanced setups.

Recommended usage:

- Default Windows flow: **PowerShell wrapper that launches Git Bash**
- Direct shell flow: **Git Bash**
- Advanced flow: **WSL** or custom auth directory override

If a custom path is invalid, the installer now:

- reports that the path is invalid
- falls back automatically
- tells you which directory is actually used

#### Windows PowerShell default install / start flow

```powershell
$script = Join-Path $env:TEMP 'cpa-manage.sh'
Invoke-WebRequest 'https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh' -OutFile $script
$bash = $null
$cmd = Get-Command bash -ErrorAction SilentlyContinue
if ($cmd) {
  $bash = $cmd.Source
}
if (-not $bash) {
  $candidates = @(
    'C:\Program Files\Git\bin\bash.exe',
    'C:\Program Files\Git\usr\bin\bash.exe',
    'C:\Program Files (x86)\Git\bin\bash.exe',
    'C:\Program Files (x86)\Git\usr\bin\bash.exe'
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      $bash = $candidate
      break
    }
  }
}
if (-not $bash) {
  Write-Error 'No usable bash executable was found. Please install Git for Windows (Git Bash), or use WSL instead.'
  return
}
& $bash $script
```

Do not treat Unix-style `bash <(curl ...)` as the default installation command for normal Windows PowerShell users.

#### Git Bash default install / start flow

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh)
```

#### Windows custom directory flow (PowerShell)

```powershell
$env:CPA_AUTH_DIR = 'D:/CPAData/auths'
$script = Join-Path $env:TEMP 'cpa-manage.sh'
Invoke-WebRequest 'https://raw.githubusercontent.com/xiaoxin-zk/cpa-account-biopsy-system/main/scripts/manage.sh' -OutFile $script
$bash = $null
$cmd = Get-Command bash -ErrorAction SilentlyContinue
if ($cmd) {
  $bash = $cmd.Source
}
if (-not $bash) {
  $candidates = @(
    'C:\Program Files\Git\bin\bash.exe',
    'C:\Program Files\Git\usr\bin\bash.exe',
    'C:\Program Files (x86)\Git\bin\bash.exe',
    'C:\Program Files (x86)\Git\usr\bin\bash.exe'
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      $bash = $candidate
      break
    }
  }
}
if (-not $bash) {
  Write-Error 'No usable bash executable was found. Please install Git for Windows (Git Bash), or use WSL instead.'
  return
}
& $bash $script
```

#### Windows custom directory flow (Git Bash)

```bash
export CPA_AUTH_DIR='D:/CPAData/auths'
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

### Repository Layout

- `cmd/cpa-account-biopsy-system/`: application entrypoint
- `internal/accounthealth/`: backend logic and embedded dashboard page
- `scripts/manage.sh`: unified install/update/uninstall/restart entrypoint
- `docker-compose.yml`: deployment definition
- `README.md`: project overview and usage

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
- It no longer requires the host-side config.yaml mount; the essential inputs are the management API and the auths directory.
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

#### Why do I need to set a password on first visit?

Because the simplified installer no longer forces you to pass a dashboard password in the command line. The first visit opens a Web initialization flow where you create the dashboard password, and the system persists it automatically.

#### Why am I forced to log in again after changing the password?

This is expected security behavior. Once the Web management password is changed, the current login session is invalidated immediately, the dashboard forces a logout, and you must sign in again with the new password. The old password and the old logged-in state both become invalid right away.

### Security Notes

- Do not commit real auth files, tokens, or management keys
- `.env` should not be committed
- Use a dedicated dashboard password

### Collaboration Entry Points

- Report bugs with the GitHub Bug Report template
- Suggest improvements with the Feature Request template
- Contribute code through a fork and Pull Request
- Check `ROADMAP.md` for `Good First Issue` and `Help Wanted` style work

### License

This repository currently includes an `MIT` license file. Please read [`LICENSE`](./LICENSE) before contributing or redistributing the project.

---

Developer: Xiaoxin  
Version: 0.2-bate
