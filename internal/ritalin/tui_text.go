package ritalin

// Only interface text is translated. Node names and model output stay intact.
var englishUI = map[string]string{
	"已取消":     "Cancelled",
	"准备截图引擎…": "Preparing screenshot engine…", "正在渲染图片…": "Rendering image…",
	"下载 Mihomo…": "Downloading Mihomo…", "Mihomo 下载失败，请检查网络连接": "Could not download Mihomo. Check your connection.",
	"测试：%s · %s / %s":     "Testing: %s · %s / %s",
	"关键词过滤：已移除候选":         "Keyword filter: candidate removed",
	"模型完成但无完整 HTML，已移除候选": "No complete HTML was produced; candidate removed",
	"[%d/%d] 检测 %s":       "[%d/%d] Checking %s", "未保存：": "Not saved: ", "出口 IP：": "Exit IP: ",
	"检测完成：%d/%d 可用": "Check complete: %d/%d reachable", "候选已保存": "Candidate saved", "探测记录：": "Probe logs: ",
	"代理": "Proxies", "探测": "Probe", "测试": "Test", "使用": "Use",
	"添加代理，自动检测可用性。":    "Add proxies; working nodes are saved automatically.",
	"使用已保存的代理获取候选状态。":  "Collect candidate states through your saved proxies.",
	"查看生成效果，保留满意的结果。":  "Review the generated results and keep the ones you like.",
	"Enter 选择，再按一次取消。": "Enter to select; press again to deselect.",
	"按需调整，其他保持默认即可。":   "Adjust what you need; leave the rest at their defaults.",
	"＋ HTTP / SOCKS":   "+ HTTP / SOCKS", "＋ Clash": "+ Clash",
	"下一步：探测 →": "Next: Probe →", "下一步：测试 →": "Next: Test →", "下一步：使用 →": "Next: Use →",
	"先添加代理 →": "Add proxies first →", "先探测状态 →": "Collect states first →",
	"清空 HTTP / SOCKS…": "Clear HTTP / SOCKS…", "清空 Clash…": "Clear Clash…",
	"高级设置 →": "Advanced →", "← 返回设置": "← Back to settings", "d 删除": "d to delete",
	"Clash 节点": "Clash", "Compact 探测": "Compact", "鹈鹕测试": "Pelican", "可用状态": "States", "设置": "Settings",
	"导航": "Navigation", "详情": "Details", "运行记录": "Activity", "实时输出": "Live output",
	"就绪": "Ready", "运行中": "Running", "待确认": "Pending", "待审核": "Review", "可用": "Usable",
	"节点": "Nodes", "状态": "State", "未选择": "None", "已选择": "Selected", "自动": "Automatic",
	"开启": "On", "关闭": "Off", "已暂停滚动": "Scroll paused", "暂无输出": "No output yet",
	"选择左侧功能开始": "Choose a section to begin", "任务记录将在这里显示": "Task activity appears here",
	"模型输出将在这里实时显示": "Model output appears here as it arrives", "等待输出…": "Waiting for output…",
	"导入节点后即可开始探测": "Import nodes to start probing", "添加状态，或从探测结果中选择": "Add a state or collect one with Compact",
	"没有待测试的状态": "No states waiting for a test", "＋ 导入节点": "+ Import nodes", "清空节点…": "Clear nodes…",
	"开始探测": "Start probing", "凭证目录": "Credentials directory", "探测模型": "Model",
	"开始 / 继续测试": "Start / resume tests", "关键词过滤": "Keyword filter", "＋ 添加状态": "+ Add state",
	"启动命令": "Launch command", "推理强度": "Reasoning effort", "截图引擎": "Screenshot engine",
	"Mihomo 路径": "Mihomo path", "上游代理": "Upstream proxy", "无沙箱渲染": "Disable rendering sandbox", "界面语言": "Language",
	"选择语言": "Choose language", "↑↓ 选择 · Enter 确认": "↑↓ choose · Enter confirm",
	"下次进入时保留此选择": "Your choice will be remembered", "切换语言": "Language",
	"编辑": "Edit", "确认": "Confirm", "取消": "Cancel", "在此输入…": "Type or paste here…",
	"保存失败：": "Could not save: ", "已保存": "Saved", "完成": "Done", "手动添加": "Manual",
	"没有未确认状态；所有进度已保存":                               "No pending states. Progress saved.",
	`格式示例：["codex"] 或 ["node","/path/to/codex.js"]`: `Use ["codex"] or ["node","/path/to/codex.js"]`,
	"请输入 low / medium / high / xhigh / max":         "Enter low / medium / high / xhigh / max",
	"此状态已存在：":                                       "State already exists: ",
	"已添加可用列表；选择该项并按 Enter 才会启用":                     "State added. Select it and press Enter to use it.",
	"关闭渲染沙箱会降低隔离保护。仍要继续吗？":                          "Disabling the rendering sandbox reduces isolation. Continue?",
	"粘贴代理列表（每行一个）或本地文件路径":                           "Paste proxy URLs (one per line) or a file path",
	"Clash YAML 文件路径或订阅 URL":                        "Clash YAML file path or subscription URL",
	"确认清空此类节点？已采集状态和实验文件保留。":                        "Clear these nodes? Saved states and files will be kept.",
	"删除此节点？对应实验和 turn-state 保留。":                    "Delete this node? Saved states and files will be kept.",
	"凭证目录 CODEX_HOME（留空使用默认目录）":                     "Credentials directory: CODEX_HOME (blank for default)",
	"探测模型（留空使用 Codex 配置）":                           "Model (blank to use your Codex configuration)",
	"粘贴 x-codex-turn-state":                         "Paste x-codex-turn-state",
	"Codex 启动命令（JSON 数组）":                           "Codex launch command (JSON array)", "鹈鹕推理强度": "Pelican reasoning effort",
	"浏览器可执行文件路径，空白自动查找/下载":     "Screenshot engine path (blank for automatic setup)",
	"Mihomo 可执行文件路径，空白自动查找/下载": "Mihomo path (blank for automatic setup)",
	"上游代理 URL（留空自动）":           "Upstream proxy URL (blank for automatic)",
	"已保留 %d 个候选，可继续测试":         "Kept %d candidates, ready for testing",
	"正在取消并保存进度…":               "Cancelling and saving progress…", "未过滤；候选已保存为未确认": "Candidates saved without filtering",
	"保留待确认结果，下次可继续":                    "Results saved for later review",
	"确认删除此 turn-state？HTML/截图和实验记录保留。": "Delete this state? HTML, screenshots, and logs will be kept.",
	"过滤候选": "Filter candidates", "按长度过滤本轮候选？": "Filter these candidates by length?",
	"账户类型": "Account type", "选择账户类型": "Choose your account type",
	"[1] 个人账户": "[1] Personal account", "[2] Team / Business": "[2] Team / Business",
	"[y] 确认   [n / Esc] 取消": "[y] Confirm   [n / Esc] Cancel",
	"审核结果":                  "Review result", "打开 HTML 或图片后选择：": "Open the HTML or image, then choose:",
	"[g] 保留   [b] 删除   [s] 稍后": "[g] Keep   [b] Delete   [s] Later",
	"Ctrl+S 保存 · Esc 返回":       "Ctrl+S save · Esc back", "Esc 取消 · PgUp/PgDn 滚动 · End 跟随": "Esc cancel · PgUp/PgDn scroll · End follow",
	"Tab 切区 · ↑↓ 选择 · Enter 操作 · d 删除 · L 语言 · q 退出": "Tab sections · ↑↓ select · Enter open · d delete · L language · q quit",
	"Tab 切区 · Enter 操作 · 空格详情 · L 语言 · q 退出":         "Tab sections · Enter open · Space details · L language · q quit",
	"Tab 切区 · Enter 操作 · L 语言":                       "Tab sections · Enter open · L language",
	"来源":                                             "Source", "模型": "Model", "长度": "Length", "密文": "Ciphertext", "块数": "Blocks",
	"出口 IP": "Exit IP", "检测时间": "Last checked", "可访问": "Reachable", "未记录": "Not recorded",
	"当前值": "Current value", "未设置": "Not set", "当前使用": "In use", "Enter 使用": "Enter to use",
	"Enter 取消选择": "Enter to deselect", "Enter 编辑": "Enter to edit", "Enter 开始": "Enter to start",
	"Enter 切换": "Enter to toggle", "Enter 删除": "Enter to delete", "数据目录": "Data directory",
	"导入 HTTP、HTTPS 或 SOCKS5 代理，自动检测并保存可用节点。": "Import HTTP, HTTPS, or SOCKS5 proxies. Working nodes are saved automatically.",
	"导入 Clash 订阅或文件，自动检测并保存可用节点。":            "Import a Clash subscription or file. Working nodes are saved automatically.",
	"删除此分类下的所有节点。":                           "Remove all nodes in this section.",
	"探测已保存节点，收集候选状态。":                        "Probe saved nodes and collect candidate states.",
	"逐个生成动画与截图，完成后由你选择保留或删除。":                "Generate an animation and screenshot for each state, then keep or delete the result.",
	"跳过首句含“内联”或“内嵌”的结果。":                     "Skip results whose first sentence contains “内联” or “内嵌”.",
	"粘贴已有状态即可加入列表。":                          "Paste an existing turn state to add it to the list.",
	"选择 Codex 的登录凭证目录。":                      "Choose the directory containing your Codex login.",
	"留空使用 Codex 配置中的模型。":                     "Leave blank to use the model in your Codex configuration.",
	"设置启动 Codex 的命令与参数。":                     "Set the command and arguments used to launch Codex.",
	"选择测试时使用的推理强度。":                          "Choose the reasoning effort for tests.",
	"留空自动查找或下载。":                             "Leave blank to find or download automatically.",
	"可选。留空使用当前网络设置。":                         "Optional. Leave blank to use your current network settings.",
	"仅在设备无法启用渲染沙箱时使用。":                       "Only use this if your device cannot run the rendering sandbox.",
	"切换界面显示语言。":                              "Choose the interface language.",
	"请将终端放大至 40 × 18":                        "Resize your terminal to at least 40 × 18",
}

func (m *ui) t(s string) string {
	return uiText(m.c.Language, s)
}

func uiText(language, s string) string {
	if language == "en" {
		if translated, ok := englishUI[s]; ok {
			return translated
		}
	}
	return s
}

func (m *ui) onOff(on bool) string {
	if on {
		return m.t("开启")
	}
	return m.t("关闭")
}

func (m *ui) stateStatus(status string) string {
	switch status {
	case "usable":
		return m.t("可用")
	case "review":
		return m.t("待审核")
	default:
		return m.t("待确认")
	}
}

func (m *ui) chooseLanguage() {
	m.languagePick = true
	m.languageCursor = 0
	if m.c.Language == "en" {
		m.languageCursor = 1
	}
}
