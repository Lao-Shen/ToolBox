# ToolBox 待办（TODO）

> 2026-08-24 记录，下次迭代做

## 工具新增（功能工具）

- [x] **鼠标右键菜单管理工具**（2026-08-26 完成）：系统分组卡片 action=contextmenu，独立窗口（?page=contextmenu）三标签（文件/文件夹/桌面空白），增删改+启停开关；禁用写 LegacyDisable 覆盖键（系统项同样生效免管理员），删除/编辑仅限当前用户项
- [x] **开机自启动管理工具**（2026-08-24 完成）：系统分组卡片 action=startup，独立窗口（?page=startup）列表管理，程序路径+来源徽标+开/关开关；关闭项本地记忆可重新开启

## 基础功能

- [ ] **拖拽添加工具**：把桌面图标/应用（.lnk/.exe 等）拖进窗口 = 添加为工具（解析图标、名称、目标路径）
- [ ] **网页添加为工具**：输入 URL 添加网页工具，图标自动取网站 favicon

## 相关现状（截至记录时）

- 工具数据目前是前端内存态（重启重置）；快捷键已持久化到 %APPDATA%\ToolBox\shortcuts.json
- 工具卡片已有右键菜单：启动/修改/删除/快捷键；修改弹窗支持图标选择（复制到 %APPDATA%\ToolBox\icons，经 /icons/ 资产路由提供）
- 全局热键已生效（HotkeyService 专用线程，WM_HOTKEY 与注册同线程）；热键支持开/关切换（窗口型工具走 WindowToggle，截图再按=取消）
- **新增工具做法见项目 CLAUDE.md「关键约定」**（常规界面工具照开机自启管理抄 WindowToggle；截图类瞬态工具才需特殊处理；热键分发层通用不改，只加注册行）
- 自动更新已实现（自研链路：清单检查 → 下载 → sha256+ed25519 校验 → 替换重启；`auto_update` 开启时静默自动下载），服务端为 https://www.earwind.top/api/v1/update/check?fullname=toolbox
