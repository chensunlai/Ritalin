# Ritalin

> **Warning:** Using this tool may result in your account being banned.

A lightweight middleware for Codex, with a TUI for modifying and setting `x-codex-turn-state` in Codex requests.

Designed primarily for **Codex CLI**. Desktop and VS Code integration is covered in the appendix.

## How it works

Select a state in the TUI. Ritalin uses it to replace the existing `x-codex-turn-state` header in requests to OpenAI and responses to Codex.

```text
Codex → Ritalin → OpenAI
```

## Install

Install Codex and sign in first. Run an installer below to install or update Ritalin.

### Linux and macOS

With curl:

```bash
curl -fsSL https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
```

Or with wget:

```bash
wget -qO- https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
```

To use `codex-ritalin` directly in new terminals, add the second line to `~/.bashrc` (or `~/.zshrc` for zsh).

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.ps1 | iex
```

Manual downloads: [Linux, macOS and Windows binaries](https://github.com/chensunlai/Ritalin/releases/latest).

## Use

Run Codex as usual:

```bash
codex-ritalin
codex-ritalin --yolo
codex-ritalin exec "Explain this project"
```

Configure Ritalin:

```bash
codex-ritalin dosing
```

Choose **中文** or **English** on first launch; your choice is saved. **L** switches languages. Follow **Proxies → Probe → Test → Use**, or add an existing state directly in **Use**.

| Section | What to do |
| --- | --- |
| Proxies | Import HTTP/SOCKS proxies or a Clash subscription/file. Working nodes are saved automatically. |
| Probe | Choose the Codex home and model, then collect candidate states through short “Reply with OK.” requests from your saved nodes. |
| Test | Watch live output and review the HTML. Press **g** to keep, **b** to delete, **s** to review later, or **p** for an optional image preview. |
| Use | Add a state or select one with **Enter**. Press **Enter** again to deselect, or **d** to delete. |
| Settings | Set the launch command, reasoning effort, and language. Optional paths and network settings are under **Advanced**. |

**Test** and **Use** both have **Import / Export** with the same JSON format: each entry in `states` contains the full `x-codex-turn-state` field. Imports join the current list, skip duplicates, and never activate a state automatically. Exports default to `ritalin/exports/` and exclude credentials, HTML, and images.

**Tab / 1–5** switches sections, **↑/↓** selects, and **Enter** confirms or saves. For multi-line proxy lists, press **Tab** to select **Save**, then **Enter**. **Esc** cancels.

For a custom launcher, set **Settings → Launch command** to a JSON array such as `["node", "/path/to/codex.js"]` or `["cmd.exe", "/c", "codex.cmd"]` on Windows.

Data lives in `~/.codex/ritalin`, or under `$CODEX_HOME/ritalin` (`CODEXHOME` is also supported). Test HTML, images, and logs are in `ritalin/pelican/`.

## Appendix: desktop and VS Code

Replace the client's backend CLI executable with `codex-ritalin`, keeping its original filename, or symlink that path to Ritalin. Reapply the replacement after desktop/extension updates; Codex CLI can help locate the executable and maintain the link.

First, set **Settings → Launch command** to a separate, unmodified Codex CLI executable—not the path being replaced—to avoid launching Ritalin recursively.

Common locations are listed below. Paths and compatibility vary by version and installation method.

| Client | Backend CLI location |
| --- | --- |
| macOS desktop | `/Applications/Codex.app/Contents/Resources/codex` |
| Windows desktop | `<installation directory>\app\resources\codex.exe`. Store packages: `C:\Program Files\WindowsApps\OpenAI.Codex_<version>\`. |
| macOS VS Code | `~/.vscode/extensions/openai.chatgpt-<version>/bin/<platform>/codex` |
| Windows VS Code | `%USERPROFILE%\.vscode\extensions\openai.chatgpt-<version>\bin\<platform>\codex.exe` |
| VS Code Remote SSH | On the remote machine: `~/.vscode-server/extensions/openai.chatgpt-<version>/bin/<platform>/codex` |
