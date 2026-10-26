import { useEffect, useRef, useState } from 'react'
import { Events, Window } from '@wailsio/runtime'
import { ContextMenuService, HotkeyService, IconExtractService, ScreenshotService, SettingsService, StartupService } from '../bindings/changeme/service'
import { Group, Settings, Tool, isImageIcon } from './types'
import GroupDialog from './GroupDialog'
import SettingsDialog from './SettingsDialog'
import ToolEditDialog from './ToolEditDialog'
import ShortcutDialog from './ShortcutDialog'
import WorkdirDialog from './WorkdirDialog'
import StartupManager from './StartupManager'
import ContextMenuManager from './ContextMenuManager'
import IconExtract from './IconExtract'

// 占位数据，后续接入后端。工具默认没有快捷键。
// 只保留已接入后端的工具（有 action）；纯展示未实现的工具已移除（见 changelog 2026-09-24）。
const GROUPS: Group[] = [
  {
    id: 'g2',
    name: '图片',
    tools: [
      { icon: '🖼', name: '区域截图', description: '框选区域截图并复制到剪贴板', shortcut: '', workdir: 'E:\\Code\\Forge\\image', action: 'screenshot' },
    ],
  },
  {
    id: 'g3',
    name: '系统',
    tools: [
      { icon: '🚀', name: '开机自启管理', description: '管理系统开机自启动程序', shortcut: '', workdir: '', action: 'startup' },
      { icon: '📋', name: '右键菜单管理', description: '管理文件/文件夹/桌面右键菜单', shortcut: '', workdir: '', action: 'contextmenu' },
      { icon: '🔧', name: '图标提取', description: '从 exe/dll 提取图标', shortcut: '', workdir: '', action: 'iconextract' },
    ],
  },
]

// 分组管理按钮图标（网格）
function GridIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="3" width="7" height="7" rx="1" />
      <rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="14" y="14" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" />
    </svg>
  )
}

// 设置按钮图标（齿轮）
function SettingsIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  )
}

// 最小化图标（横线）
function MinimizeIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  )
}

// 关闭图标（叉）
function CloseIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <line x1="6" y1="6" x2="18" y2="18" />
      <line x1="18" y1="6" x2="6" y2="18" />
    </svg>
  )
}

// 工作路径图标（文件夹）
function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
    </svg>
  )
}

// 右键菜单目标（含所属分组，供修改/删除/快捷键操作定位）
interface ContextMenuState {
  x: number
  y: number
  groupId: string
  tool: Tool
}

function App() {
  const [groups, setGroups] = useState<Group[]>(GROUPS)
  const [activeGroup, setActiveGroup] = useState(0)
  const [showGroupDialog, setShowGroupDialog] = useState(false)
  const [ctxMenu, setCtxMenu] = useState<ContextMenuState | null>(null)
  const [editingTool, setEditingTool] = useState<ContextMenuState | null>(null)
  const [shortcutTool, setShortcutTool] = useState<ContextMenuState | null>(null)
  const [workdirTool, setWorkdirTool] = useState<ContextMenuState | null>(null)

  // 删除工具 / 修改工具 / 分配快捷键
  const removeTool = (groupId: string, toolName: string) => {
    setGroups((gs) =>
      gs.map((g) =>
        g.id === groupId ? { ...g, tools: g.tools.filter((t) => t.name !== toolName) } : g,
      ),
    )
  }
  const saveTool = (groupId: string, oldName: string, updated: Tool) => {
    setGroups((gs) =>
      gs.map((g) =>
        g.id === groupId ? { ...g, tools: g.tools.map((t) => (t.name === oldName ? updated : t)) } : g,
      ),
    )
  }
  const saveShortcut = (groupId: string, toolName: string, shortcut: string) => {
    setGroups((gs) =>
      gs.map((g) =>
        g.id === groupId
          ? { ...g, tools: g.tools.map((t) => (t.name === toolName ? { ...t, shortcut } : t)) }
          : g,
      ),
    )
  }
  const saveWorkdir = (groupId: string, toolName: string, workdir: string) => {
    setGroups((gs) =>
      gs.map((g) =>
        g.id === groupId
          ? { ...g, tools: g.tools.map((t) => (t.name === toolName ? { ...t, workdir } : t)) }
          : g,
      ),
    )
  }
  // 开机自启默认关闭、关闭时隐藏到托盘默认开启；实际状态启动后从后端读取
  const [settings, setSettings] = useState<Settings>({ autoStart: false, hideToTray: true })
  const [showSettings, setShowSettings] = useState(false)
  const [settingsAutoCheck, setSettingsAutoCheck] = useState(false)
  // 已存快捷键是否已从后端回填完成：回填前不做同步，避免初次挂载的空绑定覆盖 shortcuts.json
  const [shortcutsLoaded, setShortcutsLoaded] = useState(false)
  // 轻提示
  const [toast, setToast] = useState('')
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const showToast = (message: string) => {
    setToast(message)
    clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(''), 4000)
  }

  // 点击工具卡片 → 按 action 分发到后端
  const handleToolClick = (tool: Tool) => {
    switch (tool.action) {
      case 'screenshot': {
        showToast('正在截图，请框选区域…')
        ScreenshotService.CaptureRegion(tool.workdir)
          .then((result) => {
            if (!result.path) {
              showToast('已取消截图')
              return
            }
            showToast(`已保存 ${result.width}×${result.height}，已复制到剪贴板：${result.path}`)
          })
          .catch((err) => showToast(`截图失败：${err}`))
        break
      }
      case 'startup':
        StartupService.OpenManager()
        break
      case 'contextmenu':
        ContextMenuService.OpenManager()
        break
      case 'iconextract':
        IconExtractService.OpenManager()
        break
      default:
        showToast(`「${tool.name}」尚未接入后端`)
    }
  }

  useEffect(() => {
    SettingsService.GetSettings().then(setSettings).catch(console.error)
    // 启动时回填已持久化的快捷键（按工具名匹配）
    HotkeyService.GetShortcuts()
      .then((saved) => {
        if (!saved || Object.keys(saved).length === 0) return
        setGroups((gs) =>
          gs.map((g) => ({
            ...g,
            tools: g.tools.map((t) =>
              saved[t.name] !== undefined ? { ...t, shortcut: saved[t.name] as string } : t,
            ),
          })),
        )
      })
      .catch(console.error)
      .finally(() => setShortcutsLoaded(true))
    // 快捷键触发未接入后端的工具时提示
    Events.On('hotkey', (ev: any) => {
      showToast(`快捷键已触发「${ev.data}」，该工具尚未接入`)
    })
    // 更新已就绪（自动更新下载完成 / 手动检查发现新版）→ 打开设置
    Events.On('app:update:ready', () => {
      setSettingsAutoCheck(true)
      setShowSettings(true)
    })
  }, [])

  // 快捷键变化 → 全量同步到后端注册全局热键
  // 必须先等已存快捷键回填完成（shortcutsLoaded），否则挂载时的空绑定会先把
  // shortcuts.json 覆盖成空表，导致上次设置的快捷键失效
  useEffect(() => {
    if (!shortcutsLoaded) return
    const bindings = groups
      .flatMap((g) => g.tools)
      .filter((t) => t.shortcut)
      .map((t) => ({ name: t.name, action: t.action || '', workdir: t.workdir, shortcut: t.shortcut }))
    HotkeyService.SetBindings(bindings)
      .then(() => {})
      .catch((e) => showToast(String(e)))
  }, [groups, shortcutsLoaded])

  // 开关变化 → 调后端落盘；失败时状态不变，开关视觉上弹回
  const handleSettingsChange = (next: Settings) => {
    if (next.autoStart !== settings.autoStart) {
      const apply = next.autoStart ? StartupService.Enable() : StartupService.Disable()
      apply
        .then(() => setSettings((s) => ({ ...s, autoStart: next.autoStart })))
        .catch(console.error)
    }
    if (next.hideToTray !== settings.hideToTray) {
      SettingsService.SetHideToTray(next.hideToTray)
        .then(() => setSettings((s) => ({ ...s, hideToTray: next.hideToTray })))
        .catch(console.error)
    }
  }

  const group = groups[activeGroup] || groups[0]

  // 独立窗口路由：?page=startup 开机自启管理 / ?page=contextmenu 右键菜单管理
  const page = new URLSearchParams(window.location.search).get('page')
  if (page === 'startup') {
    return <StartupManager />
  }
  if (page === 'contextmenu') {
    return <ContextMenuManager />
  }
  if (page === 'iconextract') {
    return <IconExtract />
  }

  return (
    <div className="app">
      {/* 顶部：标题 + logo，右侧窗口控制按钮 */}
      <header className="header">
        <img src="/wails.png" alt="logo" className="header-logo" />
        <span className="header-title">工具箱</span>
        <div className="win-controls">
          <button className="win-btn" aria-label="最小化" onClick={() => Window.Minimise()}>
            <MinimizeIcon />
          </button>
          <button className="win-btn win-btn-close" aria-label="关闭" onClick={() => Window.Close()}>
            <CloseIcon />
          </button>
        </div>
      </header>

      <div className="body">
        {/* 左侧：垂直分组列表，底部分组管理 + 设置按钮 */}
        <aside className="sidebar">
          <nav className="group-list">
            {groups.map((g, i) => (
              <button
                key={g.id}
                className={`group-item${i === activeGroup ? ' is-active' : ''}`}
                onClick={() => setActiveGroup(i)}
              >
                {g.name}
              </button>
            ))}
          </nav>
          <div className="settings-wrap">
            <button className="settings-btn" title="分组管理" onClick={() => setShowGroupDialog(true)}>
              <GridIcon />
              <span>分组</span>
            </button>
            <button className="settings-btn" title="设置" onClick={() => { setSettingsAutoCheck(false); setShowSettings(true) }}>
              <SettingsIcon />
              <span>设置</span>
            </button>
          </div>
        </aside>

        {/* 右侧：内容区（圆角），一个卡片代表一个工具 */}
        <main className="content">
          {group ? (
            <>
              {group.tools.length === 0 && (
                <div className="empty">该分组暂无工具</div>
              )}
              {group.tools.map((tool) => (
                <article
                  className="tool-card"
                  key={tool.name}
                  onClick={() => handleToolClick(tool)}
                  onContextMenu={(e) => {
                    e.preventDefault()
                    // 菜单贴边自动收回屏幕内
                    const x = Math.min(e.clientX, window.innerWidth - 150)
                    const y = Math.min(e.clientY, window.innerHeight - 180)
                    setCtxMenu({ x, y, groupId: group.id, tool })
                  }}
                >
                  <div className="tool-head">
                    {isImageIcon(tool.icon) ? (
                      <img className="tool-img" src={tool.icon} alt="" />
                    ) : (
                      <span className="tool-icon">{tool.icon}</span>
                    )}
                    <span className="tool-name">{tool.name}</span>
                    {tool.shortcut && <kbd className="tool-shortcut">{tool.shortcut}</kbd>}
                  </div>
                  <p className="tool-desc">{tool.description}</p>
                  <div className="tool-workdir">
                    <FolderIcon />
                    <span title={tool.workdir}>{tool.workdir}</span>
                  </div>
                </article>
              ))}
            </>
          ) : (
            <div className="empty">暂无分组，点击左下角“分组”新建</div>
          )}
        </main>
      </div>

      {/* 分组管理弹窗 */}
      {showGroupDialog && (
        <GroupDialog
          groups={groups}
          onChange={setGroups}
          onClose={() => setShowGroupDialog(false)}
        />
      )}

      {/* 设置弹窗 */}
      {showSettings && (
        <SettingsDialog
          settings={settings}
          onChange={handleSettingsChange}
          onClose={() => setShowSettings(false)}
          autoCheck={settingsAutoCheck}
        />
      )}

      {/* 卡片右键菜单：启动 / 修改 / 删除 / 快捷键 */}
      {ctxMenu && (
        <>
          <div
            className="ctx-cover"
            onClick={() => setCtxMenu(null)}
            onContextMenu={(e) => {
              e.preventDefault()
              setCtxMenu(null)
            }}
          />
          <div className="ctx-menu" style={{ left: ctxMenu.x, top: ctxMenu.y }}>
            <button
              className="ctx-item"
              onClick={() => {
                handleToolClick(ctxMenu.tool)
                setCtxMenu(null)
              }}
            >
              启动
            </button>
            <button
              className="ctx-item"
              onClick={() => {
                setWorkdirTool(ctxMenu)
                setCtxMenu(null)
              }}
            >
              工作目录
            </button>
            <button
              className="ctx-item"
              onClick={() => {
                setEditingTool(ctxMenu)
                setCtxMenu(null)
              }}
            >
              修改
            </button>
            <button
              className="ctx-item"
              onClick={() => {
                removeTool(ctxMenu.groupId, ctxMenu.tool.name)
                setCtxMenu(null)
              }}
            >
              删除
            </button>
            <button
              className="ctx-item"
              onClick={() => {
                setShortcutTool(ctxMenu)
                setCtxMenu(null)
              }}
            >
              快捷键
            </button>
          </div>
        </>
      )}

      {/* 工作目录弹窗 */}
      {workdirTool && (
        <WorkdirDialog
          tool={workdirTool.tool}
          onSave={(workdir) => {
            saveWorkdir(workdirTool.groupId, workdirTool.tool.name, workdir)
            setWorkdirTool(null)
          }}
          onClose={() => setWorkdirTool(null)}
        />
      )}

      {/* 修改工具弹窗 */}
      {editingTool && (
        <ToolEditDialog
          tool={editingTool.tool}
          onSave={(updated) => {
            saveTool(editingTool.groupId, editingTool.tool.name, updated)
            setEditingTool(null)
          }}
          onClose={() => setEditingTool(null)}
        />
      )}

      {/* 快捷键分配弹窗 */}
      {shortcutTool && (
        <ShortcutDialog
          current={shortcutTool.tool.shortcut}
          onSave={(s) => {
            saveShortcut(shortcutTool.groupId, shortcutTool.tool.name, s)
            setShortcutTool(null)
          }}
          onClose={() => setShortcutTool(null)}
        />
      )}

      {/* 轻提示 */}
      {toast && (
        <div className="toast" role="status">
          <span>{toast}</span>
        </div>
      )}
    </div>
  )
}

export default App
