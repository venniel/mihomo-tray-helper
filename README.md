# mihomo tray helper

> 本项目是 [sing-box-drover](https://github.com/hdrover/sing-box-drover) 的 fork，在其基础上做了二次修改。感谢原作者的开源工作。

mihomo tray helper（MTH）是 Windows 上的轻量托盘控制器：后台运行官方 `mihomo` 核心，对外只提供少量原生控制入口——系统代理、TUN、模式切换、节点切换和面板链接。

目标是保留 mihomo 配置的完整能力，只补 Windows 相关的胶水代码，不做成又一个一体化 GUI 客户端。

- 使用官方 `mihomo.exe`，不内嵌、不打包核心
- 原样使用一个 YAML 配置
- 原生 Win32 控件（不依赖 GUI 框架），控件带明确的无障碍名称，NVDA、争渡读屏等读屏软件可操作托盘菜单和设置对话框
- 空闲开销小：启动后不轮询 HTTP，仅在启动、打开菜单和切换开关时访问 Clash API

## 功能

- 托盘图标反映当前状态：TUN 生效、系统代理开启或空闲
- 左键单击托盘图标打开设置对话框
- 右键菜单：
    - **更多** — 重启、开机自启、项目主页
    - **模式** — rule / global / direct（配置含 `script` 时另有 script）
    - **节点** — `select` 代理组；当前被核心使用的成员会打勾，包括在面板等外部途径更改的选择
    - **TUN 模式** — 配置含 `tun` 段时可用，需要时请求提权
    - **系统代理** — 切换 Windows 代理设置
    - **面板** — 打开 Clash 面板
    - **退出** — 正常停止核心并退出
- 正常退出时关闭系统代理；上次未关闭的代理状态会在下次启动时恢复
- TUN 状态跨重启保留；开启 TUN 会以提权方式（UAC）重启程序
- 每个用户会话只允许一个实例，重复启动会被拒绝
- 可选通过计划任务实现开机自启

## 运行要求

- Windows 10 及以上（64 位）
- 官方 Windows 版 `mihomo`，从上游项目下载：<https://github.com/MetaCubeX/mihomo/releases>。文件可保留下载名（如 `mihomo-windows-amd64-v1.19.33.exe`），在设置对话框中选择即可
- TUN 模式需要可用的 `wintun.dll` 和管理员权限
- 满足下述要求的 mihomo YAML 配置

## 快速开始

1. 用 `.\build.ps1` 构建，或执行：

   ```sh
   go build -ldflags "-H=windowsgui -s -w" -trimpath -o MTH.exe .
   ```

   安装了 `rsrc` 时，`build.ps1` 会重新生成仅包含图标的 Windows 资源。

2. 将 `MTH.exe` 放到任意目录。
3. 运行 `MTH.exe`。首次启动（程序目录没有 `MTH.ini`）会弹出设置对话框，选择核心可执行文件和配置文件后即可使用。若希望跳过首次设置，可将仓库中的 `MTH.ini.example` 复制为程序目录下的 `MTH.ini`，它默认指向同目录的 `mihomo.exe` 和 `config.yaml`。左键单击托盘图标可随时打开设置对话框；删除 `MTH.ini`（以及存在的旧文件 `mihomo-go.ini`）可再次进入首次设置。

托盘菜单和设置对话框为中文。

## mihomo 配置要求

### 必须：代理端口

程序依次读取 `mixed-port`、`port`、`socks-port` 来配置 Windows 系统代理。监听地址取自 `bind-address`，通配值（`*`、`0.0.0.0`、`::`）会映射为 `127.0.0.1`。

```yaml
mixed-port: 7890
bind-address: '*'
```

### 可选：tun 段

配置含顶层 `tun` 段时显示 TUN 菜单项。TUN 通过改写内存中配置副本的 `tun.enable` 启动，磁盘文件不会被修改。

```yaml
tun:
  enable: true
  stack: mixed
  auto-route: true
```

禁用 TUN 时，核心使用 `tun.enable` 为 `false` 的临时配置副本启动。

### 可选：Clash API

节点切换、模式切换和面板链接依赖 Clash API：

```yaml
external-controller: '127.0.0.1:9090'
secret: change-me
external-ui: ui # 可选
```

缺少 `external-controller` 时，程序以 `-ext-ctl 127.0.0.1:9090 -secret 1234` 启动核心，不重写配置文件。

**面板**菜单项的行为：

- 设置了 `external-ui`：打开本地面板 `http://<controller>/ui/`
- 未设置 `external-ui`：打开在线 Yacd-meta <https://venniel.github.io/Yacd-meta>，controller 主机、端口和 secret 作为 URL 参数
- `external-ui` 为 `http(s)://` 地址：按原值打开

### 可选：节点切换用的代理组

`type: select` 的组会显示在**节点**菜单中。成员列表和当前选择来自 `GET /proxies`，因此 `proxy-providers` / `use` 支持的组以及在面板中的改动同样生效。

```yaml
proxy-groups:
  - name: Proxy
    type: select
    proxies: [Direct]
    use: [provider]
```

切换节点会向 Clash API 发送 `PUT /proxies/<group>`，随后关闭现有连接，改动立即生效。

### 模式

**模式**菜单提供 `rule`、`global`、`direct`，配置含 `script` 段时增加 `script`。选择模式会发送 `PATCH /configs`。

## 程序配置

设置保存在可执行文件旁的 `MTH.ini`。仓库只提供模板 `MTH.ini.example`，实际使用的 `MTH.ini` 是运行期文件，已被 `.gitignore` 忽略。首次启动时若存在 `mihomo-go.ini`，会复制为 `MTH.ini`（旧文件保留），旧设置继续生效：

```ini
[MTH]
mihomo-core = mihomo.exe
config-file = config.yaml
tun-start-mode = off
system-proxy-auto = 0
; 节点菜单布局："auto"、"flat" 或 "nested"
selector-menu-layout = auto
; 可选：写入日志文件（相对路径按程序目录解析）
; log-file = mth.log
```

| 键 | 说明 |
| --- | --- |
| `mihomo-core` | 核心可执行文件的完整路径，不要求命名为 `mihomo.exe`。 |
| `config-file` | mihomo YAML 配置的完整路径（`.yaml` 或 `.yml`）。 |
| `tun-start-mode` | 当前 TUN 状态：`on` / `off`。切换时自动更新，启动时生效。 |
| `system-proxy-auto` | 当前系统代理状态：`1` 开启，`0` 关闭。切换时自动更新，启动时生效。 |
| `selector-menu-layout` | 节点菜单布局：`auto` 按数量决定，`flat` 每组为禁用标题加可勾选项，`nested` 使用子菜单。 |
| `log-file` | 可选日志路径，相对路径按程序目录解析；留空则不记日志。 |

设置对话框可编辑 `mihomo-core`、`config-file` 和 `selector-menu-layout`；手动输入的相对路径按程序目录解析。保存不同的核心或配置文件后程序会自动重启。

### 运行时状态

TUN 和系统代理的选择变化会即时写回 `MTH.ini`，不使用独立状态文件。正常退出时关闭系统代理但保留偏好，下次启动时恢复。

### 开机自启

自启由计划任务 `MTH` 实现：登录时以最高可用权限运行当前可执行文件。启用或禁用可能触发 UAC。可执行文件移动后需重新启用自启，使任务指向新路径。

## 命令行参数

主要用于程序自我重启，也可手动传入：

| 参数 | 作用 |
| --- | --- |
| `-tun` | 以 TUN 开启状态启动（需要时请求提权）。 |
| `-restart` | 等待已运行实例退出后再启动。 |
| `-autostart-enable` | 创建自启计划任务。 |
| `-autostart-disable` | 删除自启计划任务。 |

## 核心的启动方式

核心以 `<所选核心> -f <临时配置> -d <核心目录>\config` 启动。临时配置是所选 YAML 的副本，其中 `tun.enable` 被强制为目标状态，原文件不会被修改。数据目录建在核心旁边，存放 `cache.db`、geo 数据库、provider 和规则集，保持配置文件所在目录整洁。若配置中相对的 `external-ui` 目录存在于配置文件旁，会通过 `-ext-ui` 传入，面板可继续使用。

## 构建与测试

要求：

- Go 1.27 或更高版本
- `golang.org/x/sys` 和 `gopkg.in/yaml.v3`（由 `go mod download` 获取）
- 可选：`rsrc`，仅用于重新生成图标资源

```powershell
.\build.ps1
```

或手动执行：

```sh
go build -ldflags "-H=windowsgui -s -w" -trimpath -o MTH.exe .
go test .
```

`-H=windowsgui` 会隐藏控制台窗口。UI 直接基于 Win32（GDI/user32/shell32）编写，除 Windows 系统库外没有运行时依赖。

### Windows 资源

`resource_windows_amd64.syso` 只包含可执行文件图标，由 [rsrc](https://github.com/akavel/rsrc) 从 `icon.ico` 生成：

```sh
rsrc -arch amd64 -ico icon.ico -o resource_windows_amd64.syso
```

该 .syso 已提交，直接 `go build` 即可得到带图标的 `MTH.exe`。程序不再嵌入版本号、公司、描述或应用清单，因此资源管理器中不会显示这些信息；启动权限仍由系统默认的 asInvoker 决定，TUN 仍通过 UAC 显式提权。

### 图标资源

`icon.svg` 是托盘、窗口和可执行文件图标的矢量母版。`make-icons.ps1` 使用 [resvg-cli](https://github.com/Zhengqbbb/resvg-cli)（`npm install -g resvg-cli`）渲染它，并重新生成根目录的 `icon.ico`（16/20/24/32/48/64/128/256 px 帧）以及 `assets/` 下的 `icon.png`、`icon_512.png`。更新 `icon.ico` 后运行 `build.ps1`（或手动执行上面的 `rsrc` 命令）可同步更新可执行文件图标。

### 源码结构

| 文件 | 用途 |
| --- | --- |
| `main.go` | 启动、单实例、DPI/COM 初始化、首次运行设置、提权 |
| `ui.go` | 主窗口、消息循环、系统代理与 TUN 操作、自启、设置入口 |
| `tray.go` | 托盘图标、状态提示与气泡通知 |
| `menu.go` | 右键菜单构建与命令分发 |
| `shutdown.go` | 关闭窗口、会话结束与退出看门狗 |
| `settings.go` | Win32 设置对话框和文件选择器 |
| `win32.go` | Win32 绑定、结构体、无障碍名称、辅助函数 |
| `drover.go` | 应用状态、节点/模式状态、运行时持久化、退出 |
| `supervisor.go` | 核心监督循环、状态与命令分发 |
| `coreprocess.go` | 核心进程启动、停止与临时配置 |
| `coreapi.go` | Clash API 查询与切换操作 |
| `coreoutput.go` | 核心输出捕获与关键错误提取 |
| `apiclient.go` | 精简的 Clash API 客户端 |
| `config.go` | YAML 解析、要求检查、TUN 改写、Clash API 探测 |
| `text.go` | 文本编码探测（UTF-8、UTF-16、ANSI） |
| `options.go` | INI 选项和运行时状态 |
| `autostart.go`、`elevation.go`、`singleinstance.go` | Windows 集成辅助 |
| `systemproxy.go`、`tuncleanup.go`、`logger.go` | 系统代理、Wintun 清理、日志 |
| `resource_windows_amd64.syso` | 仅图标资源（由 rsrc 生成） |
| `icon.svg`、`make-icons.ps1` | 矢量图标母版与 ICO/PNG 生成脚本 |
| `assets/` | 仓库展示用 PNG；运行期只嵌入 `icon.ico` |
| `MTH.ini.example` | 设置文件模板；复制为 `MTH.ini` 后使用 |
| `build.ps1` | 重新生成 Windows 资源并构建 `MTH.exe` |

## 限制

- 一次只能使用一个配置文件，UI 不提供配置切换。
- 节点选择不由本程序保存，是否跨重启保留取决于 mihomo 的 `cache.db`。
- TUN 模式需要提升权限，且配置中必须有 `tun` 段。
- 本程序不修改 mihomo 配置文件，TUN 改动仅对本次运行有效。
