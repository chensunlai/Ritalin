# Ritalin-利他林

Codex 的轻量包装器。先安装并登录 Codex，再用 Go 1.24+ 构建：

```bash
go build -trimpath -ldflags="-s -w" -o codex-ritalin .
./codex-ritalin dosing
```

将二进制放到 PATH 后，像使用 Codex 一样使用它。普通参数、标准输入输出、工作目录和退出码透传；只有 `dosing` 进入管理界面。Windows 的输出名用 `codex-ritalin.exe`。

```bash
codex-ritalin
codex-ritalin --yolo
codex-ritalin exec "解释这个项目"
codex-ritalin dosing
```

界面中 `Tab` / `1–6` 切换功能区，方向键选择，`Enter` 操作，输入框 `Ctrl+S` 提交，`Esc` 返回或取消任务。

1. **HTTP / SOCKS**：粘贴 `http://`、`https://`、`socks5://` 代理列表（支持认证、IPv6），或填写列表文件路径。探测 OpenAI / ChatGPT models，至少一个入口可达才保存。JSON 401 仅表示到达鉴权入口，不等于登录成功。不跟随系统代理、不失败后直连。可删除单个节点或清空本类。
2. **Clash 节点**：填写 YAML 文件路径或订阅 URL，只提取 `proxies`，不加载订阅规则/TUN/provider。没有 Mihomo 时自动下载并校验 SHA-256；下载使用系统代理，节点运行不继承系统代理。显示耗时和下载进度，支持取消、删除、清空。
3. **Compact 探测**：可指定凭证 `CODEX_HOME` 和模型，对所有保存节点最多并发 **2** 个请求。Mihomo 临时启动，结束即关闭。保存事件、turn-state、字符数、密文字节数及 16 字节块数。完成后可选快速过滤：个人号保留 **292 字符 / 10 块**；Team/Business 保留 **332 字符 / 12 块**。只过滤本轮，结果仍为“未确认”。
4. **鹈鹕测试**：使用系统代理逐个测试，默认 `low`，实时显示并保存模型输出。首个请求注入待测状态，其后双向替换。可开启首句“内联 / 内嵌”快速排除。自动保存 HTML 和渲染 PNG，显示路径；`g` 确认可用，`b` 删除候选，`s` 稍后确认。`Esc` 停止并保留进度。网络/渲染失败保留候选供重试；模型正常结束但没有完整 HTML 才自动删除。
5. **可用状态**：直接粘贴已有 turn-state，或选择人工确认过的状态。`Enter` 选为当前值，`d` 删除；下次启动 Codex 生效。关闭“双向替换”可用相同 warp 链路做对照；“停用当前状态”则直接启动 Codex。
6. **设置**：配置 Codex 启动命令、探测凭证目录、模型、推理强度、日常 warp 上游。命令使用 JSON 数组，例如 `["codex"]`、`["node","/path/to/codex.js"]` 或 `["cmd.exe","/c","codex.cmd"]`，不依赖 shell 别名。

配置、下载和结果位于 `$CODEX_HOME/ritalin`；兼容 `$CODEXHOME/ritalin`，两者同时设置以前者为准；否则使用 `~/.codex/ritalin`。界面的“探测 HOME”仅决定凭证来源，不移动数据目录，也不改变日常 Codex 登录。

```text
ritalin/
  config.json    设置、代理、候选和当前选择（敏感）
  clash/         Mihomo 下载和临时运行文件
  ca/            本地 CA 与私钥
  probes/        每轮 compact 事件、统计
  pelican/       测试工作空间；各轮 HTML、PNG、模型输出、事件
  browser/       按需下载的截图引擎
```

探测读取指定 HOME 的 `auth.json`，需要 ChatGPT 登录凭证；不支持 keyring-only 凭证，不自动刷新令牌。请先用 Codex 登录，凭证存储设置为 `cli_auth_credentials_store = "file"`。模型读取该 HOME 的 `config.toml`，也可在界面指定。不预置或复制账号、订阅、turn-state。

日常链路是 **Codex → 本地 warp → 系统代理/设置的上游**。只有选中可用状态才启用 warp，双向替换目标 HTTPS 请求/响应中已有的头，不改流式正文或 WebSocket 消息。仅给本次 Codex 设置[自定义 CA](https://learn.chatgpt.com/docs/auth)，不安装系统证书，不关闭上游 TLS 校验。系统代理指环境变量（包括 `ALL_PROXY` / `NO_PROXY`），不读取系统 PAC。绕过环境代理的客户端不保证经过 warp。

截图全自动完成，不需手动安装 Python、Node、Playwright 或 mitmproxy；截图引擎不存在时后台下载。渲染禁止页面 JavaScript 和外部资源。受限服务器若不支持浏览器沙箱，可修复系统配置，或明确确认高级设置中的无沙箱渲染；默认不关闭沙箱。HTML 和图片直接用浏览器或文件管理器查看。

长度、块数和关键词是**实验启发式，不是官方质量判据**，布局解析不是解密或 MAC 验证。拿到状态与 compact 完成分别记录。固定状态可能过期或绑定账号；建议同账号、模型、提示词分别新建会话对照。实时面板只展示实际可见输出，不恢复隐藏推理。compact 不自动重试；Codex 自身发出的重试事件会记录。

不要上传 `ritalin/`、凭证、节点密码、完整 turn-state 或 CA 私钥。仓库不包含真实实验数据。运行 `go test -race ./...` 做离线测试；可设置 `RITALIN_TEST_BROWSER` 执行实际截图测试。

源码使用 **Apache-2.0**；独立下载的 [Mihomo](https://github.com/MetaCubeX/mihomo) 和截图引擎使用各自许可证。
