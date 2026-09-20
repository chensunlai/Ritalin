# Ritalin

A lightweight Codex wrapper for testing proxies, comparing turn states, and choosing the state used by your next session.

## How it works

`codex-ritalin` forwards your commands to Codex. Run `codex-ritalin dosing` to manage proxies, collect `x-codex-turn-state` values, and compare them with an animated pelican-on-a-bicycle test.

When a state is selected, a local wrapper replaces that header in both directions:

```text
Codex → Ritalin → OpenAI
```

Turn replacement off to run a comparison through the same connection.

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

Arguments, input, output, and the working directory are passed through to Codex.

## Configure with dosing

```bash
codex-ritalin dosing
```

Use **Tab** or **1–6** to switch sections, **↑/↓** to select, and **Enter** to open an action. Submit input with **Ctrl+S**. **Esc** cancels the current task and keeps completed progress.

| Section | What to do |
| --- | --- |
| HTTP / SOCKS | Paste proxy URLs, one per line, or enter a file path. Ritalin tests the models endpoints and saves reachable proxies. Delete individual entries or clear the list. |
| Clash | Enter a subscription URL or YAML file path. Ritalin downloads Mihomo if needed, extracts the nodes, tests them, and saves reachable ones. |
| Compact | Choose the Codex home and model, then probe all saved nodes with up to two concurrent requests. Inspect state lengths and optionally filter the new candidates. |
| Pelican | Test candidates one at a time with live output. Open the generated HTML and PNG, press **g** to keep, **b** to reject, or **s** to review later. Resume whenever you want. |
| Usable states | Paste an existing state directly, select a tested state with **Enter**, or delete one with **d**. Toggle replacement for comparison runs. |
| Settings | Change the Codex launch command, model, reasoning effort, proxy, and optional executable paths. |

Proxy checks and compact probes use each chosen node directly. Downloads and pelican tests connect directly unless a system proxy is configured. Mihomo starts and stops automatically around node tests.

Pelican tests default to `low`. The optional first-sentence filter skips responses containing `内联` or `内嵌`. Each completed test produces an HTML file and an automatically rendered image.

The optional length filter keeps **292 characters / 10 blocks** for personal accounts, or **332 characters / 12 blocks** for Team/Business accounts. Candidates still need your review.

If `codex` is not directly executable on your machine, set a launch command as a JSON array:

```json
["node", "/path/to/codex.js"]
```

On Windows, for example:

```json
["cmd.exe", "/c", "codex.cmd"]
```

Settings, downloads, and results live in `~/.codex/ritalin`, or under `$CODEX_HOME/ritalin` when set. `CODEXHOME` is also supported. Find generated HTML, images, and output logs in `ritalin/pelican/`.
