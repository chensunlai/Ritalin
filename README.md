# Ritalin-利他林

开发中。需要已安装并登录的 Codex。构建单一二进制：

```bash
go build -o codex-ritalin .
./codex-ritalin dosing
```

普通参数原样交给 Codex，例如 `codex-ritalin --yolo`；`dosing` 管理代理、探测、鹈鹕验证与状态选择。

配置和下载保存在 `$CODEX_HOME/ritalin`，兼容 `$CODEXHOME/ritalin`，未设置时使用 `~/.codex/ritalin`。这些本机数据不应上传 GitHub。
