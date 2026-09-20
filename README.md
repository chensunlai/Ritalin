# Ritalin

A lightweight middleware for Codex, with a TUI for modifying and setting `x-codex-turn-state` in Codex requests.

Built primarily for **Codex CLI**. To use Ritalin with the desktop app or VS Code extension, manually replace its backend CLI executable with `codex-ritalin` (keeping the original filename), or symlink that executable path to Ritalin. Maintain the replacement or symlink yourself after desktop/extension updates. You can ask Codex CLI to help locate the executable and perform these steps.

Before replacing it, set **Settings → Launch command** to a separate, unmodified Codex CLI executable—not the path you are replacing—so Ritalin does not launch itself.

Common locations to check; version and platform directories vary:

| Client | Backend CLI location |
| --- | --- |
| [macOS desktop](https://learn.chatgpt.com/docs/reference/troubleshooting) | `/Applications/Codex.app/Contents/Resources/codex` |
| Windows desktop | `<installation directory>\app\resources\codex.exe`. Microsoft Store installations are commonly under `C:\Program Files\WindowsApps\OpenAI.Codex_<version>\`. |
| macOS VS Code extension | `~/.vscode/extensions/openai.chatgpt-<version>/bin/<platform>/codex` |
| Windows VS Code extension | `%USERPROFILE%\.vscode\extensions\openai.chatgpt-<version>\bin\<platform>\codex.exe` |
| VS Code Remote SSH | On the **remote machine**: `~/.vscode-server/extensions/openai.chatgpt-<version>/bin/<platform>/codex` |

Keep the backend CLI compatible with the client version. Replacement is manual integration, not guaranteed desktop/extension compatibility; app signing or WindowsApps permissions may restrict it.

## How it works

When a state is selected, Ritalin replaces the existing `x-codex-turn-state` header in Codex's requests before forwarding them to OpenAI. It applies the same replacement to responses before returning them to Codex.

```text
Codex → Ritalin → OpenAI
```

## Install

Install and log in to Codex first. These scripts download the latest binary and detect your platform automatically. Run the same command again to update.

### Linux and macOS

With curl:

```bash
curl -fsSL https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
```

With wget:

```bash
wget -qO- https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
```

To use `codex-ritalin` directly in new terminals, add the second line to `~/.bashrc` (or `~/.zshrc` for zsh).

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.ps1 | iex
```

### Choose a version or directory

```bash
curl -fsSL https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.sh |
  RITALIN_VERSION=v0.1.0 RITALIN_INSTALL_DIR="$HOME/.local/bin" sh
```

```powershell
$env:RITALIN_VERSION = 'v0.1.0'
irm https://raw.githubusercontent.com/chensunlai/Ritalin/main/install.ps1 | iex
```

[Download manually](https://github.com/chensunlai/Ritalin/releases/latest): Linux x64, ARM64, x86 and ARMv7; macOS Intel and Apple Silicon; Windows x64, ARM64 and x86.

## Use Codex normally

```bash
codex-ritalin
codex-ritalin --yolo
codex-ritalin exec "Explain this project"
```

## Configure with dosing

```bash
codex-ritalin dosing
```

Choose **中文** or **English** on entry; press **L** to switch later.

Follow **Proxies → Probe → Test → Use**. Already have a state? Add it directly in **Use**.

Use **Tab** or **1–5** to switch sections, **↑/↓** to select, and **Enter** to act. Submit input with **Ctrl+S**. **Esc** cancels a task and keeps completed progress. On narrower terminals, **Space** opens details. **PgUp/PgDn** scroll output; **End** returns to live output.

| Section | What to do |
| --- | --- |
| Proxies | Add HTTP/SOCKS proxies or a Clash subscription/file. Working nodes are saved automatically. Press **d** to delete a node. |
| Probe | Choose the Codex home and model, then collect candidate states from your saved nodes. |
| Test | Watch live output and review the generated HTML and PNG. Press **g** to keep, **b** to delete, or **s** to review later. |
| Use | Add a state or select one with **Enter**. Press **Enter** again to deselect, or **d** to delete. |
| Settings | Set the Codex launch command, reasoning effort, and language. Optional paths and network settings are under **Advanced**. |

If `codex` is not directly executable on your machine, set a launch command as a JSON array:

```json
["node", "/path/to/codex.js"]
```

On Windows, for example:

```json
["cmd.exe", "/c", "codex.cmd"]
```

Settings, downloads, and results live in `~/.codex/ritalin`, or under `$CODEX_HOME/ritalin` when set. `CODEXHOME` is also supported. Find generated HTML, images, and output logs in `ritalin/pelican/`.
