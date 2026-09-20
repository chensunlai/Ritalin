# Ritalin

A lightweight middleware for Codex, with a TUI for collecting, testing, and selecting turn states.

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

Use **Tab** or **1–6** to switch sections, **↑/↓** to select, and **Enter** to open an action. Submit input with **Ctrl+S**. **Esc** cancels the current task and keeps completed progress.

| Section | What to do |
| --- | --- |
| HTTP / SOCKS | Paste proxy URLs, one per line, or enter a file path to test and save working proxies. Delete entries or clear the list. |
| Clash | Import a subscription URL or YAML file to test and save working nodes. Mihomo is downloaded automatically if needed. |
| Compact | Choose the Codex home and model, then probe saved nodes to collect turn-state candidates. |
| Pelican | Test candidates with live output and review the generated HTML and PNG. Press **g** to keep, **b** to reject, or **s** to review later. |
| Usable states | Paste a state directly, select one with **Enter**, or delete one with **d**. |
| Settings | Change the Codex launch command, model, reasoning effort, proxy, and optional executable paths. |

If `codex` is not directly executable on your machine, set a launch command as a JSON array:

```json
["node", "/path/to/codex.js"]
```

On Windows, for example:

```json
["cmd.exe", "/c", "codex.cmd"]
```

Settings, downloads, and results live in `~/.codex/ritalin`, or under `$CODEX_HOME/ritalin` when set. `CODEXHOME` is also supported. Find generated HTML, images, and output logs in `ritalin/pelican/`.
