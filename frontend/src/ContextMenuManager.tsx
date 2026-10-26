import { useEffect, useState } from 'react'
import { ContextMenuService, type ContextMenuEntry } from '../bindings/changeme/service'
import { CloseIcon } from './icons'

// 右键菜单管理页面（独立窗口，?page=contextmenu 路由）
// 三个分区：文件右键 / 文件夹右键 / 桌面空白；每项可启停、编辑、删除（系统项仅可启停）

const SECTIONS = [
  { id: 'file', label: '文件右键' },
  { id: 'folder', label: '文件夹右键' },
  { id: 'desktop', label: '桌面空白' },
]

function ContextMenuManager() {
  const [section, setSection] = useState('file')
  const [items, setItems] = useState<ContextMenuEntry[]>([])
  const [error, setError] = useState('')
  const [showDisabled, setShowDisabled] = useState(true)
  // 弹窗：null 关闭；item 为 null 表示新增，否则编辑该项
  const [dialog, setDialog] = useState<{ item: ContextMenuEntry | null } | null>(null)
  const [dName, setDName] = useState('')
  const [dCommand, setDCommand] = useState('')
  const [formErr, setFormErr] = useState('')
  const [confirmKey, setConfirmKey] = useState('') // 删除确认中的键名

  const refresh = () => {
    ContextMenuService.List(section)
      .then((list) => setItems(list || []))
      .catch((e) => setError(String(e)))
  }
  useEffect(refresh, [section])

  const toggle = (it: ContextMenuEntry, enabled: boolean) => {
    setError('')
    ContextMenuService.SetEnabled(section, it.key, enabled)
      .then(refresh)
      .catch((e) => setError(`操作失败：${e}`))
  }

  const del = (it: ContextMenuEntry) => {
    setError('')
    setConfirmKey('')
    ContextMenuService.Delete(section, it.key)
      .then(refresh)
      .catch((e) => setError(`删除失败：${e}`))
  }

  const openAdd = () => {
    setDName('')
    setDCommand('')
    setFormErr('')
    setDialog({ item: null })
  }

  const openEdit = (it: ContextMenuEntry) => {
    setDName(it.name)
    setDCommand(it.command)
    setFormErr('')
    setDialog({ item: it })
  }

  const save = () => {
    if (!dName.trim() || !dCommand.trim()) {
      setFormErr('名称和命令都不能为空')
      return
    }
    const key = dialog?.item?.key || ''
    ContextMenuService.Save(section, key, dName.trim(), dCommand.trim())
      .then(() => {
        setDialog(null)
        refresh()
      })
      .catch((e) => setFormErr(String(e)))
  }

  const enabled = items.filter((it) => it.enabled)
  const disabled = items.filter((it) => !it.enabled)

  const renderRow = (it: ContextMenuEntry) => (
    <div className={`startup-row cm-row${it.enabled ? '' : ' is-disabled'}`} key={it.key}>
      <div className="startup-main">
        <span className="startup-name" title={it.name}>{it.name}</span>
        <span className="startup-path" title={it.command}>{it.command}</span>
      </div>
      {it.extended && (
        <span className="cm-badge" title="按住 Shift 右键才显示">
          Shift
        </span>
      )}
      <span className={`startup-source${it.system ? ' is-hklm' : ''}`}>
        {it.system ? '所有用户' : '当前用户'}
      </span>
      {confirmKey === it.key ? (
        <div className="confirm-strip">
          <button className="confirm-btn confirm-btn-danger" onClick={() => del(it)}>
            确认删除
          </button>
          <button className="confirm-btn" onClick={() => setConfirmKey('')}>
            取消
          </button>
        </div>
      ) : (
        <div className="cm-actions">
          <button
            className="row-btn"
            disabled={it.system}
            title={it.system ? '系统项修改需要管理员权限' : '编辑'}
            onClick={() => openEdit(it)}
          >
            编辑
          </button>
          <button
            className="row-btn row-btn-danger"
            disabled={it.system}
            title={it.system ? '系统项删除需要管理员权限' : '删除'}
            onClick={() => setConfirmKey(it.key)}
          >
            删除
          </button>
        </div>
      )}
      <label className="toggle">
        <input type="checkbox" checked={it.enabled} onChange={(e) => toggle(it, e.target.checked)} />
        <span className="slider" />
      </label>
    </div>
  )

  return (
    <div className="app">
      <header className="header">
        <span className="header-title">右键菜单管理</span>
        <div className="cm-toolbar">
          <button className="btn-primary" onClick={openAdd}>
            新增菜单项
          </button>
        </div>
      </header>

      <div className="cm-tabs">
        {SECTIONS.map((s) => (
          <button
            key={s.id}
            className={`cm-tab${s.id === section ? ' is-active' : ''}`}
            onClick={() => {
              setSection(s.id)
              setConfirmKey('')
              setError('')
            }}
          >
            {s.label}
          </button>
        ))}
      </div>

      <main className="content startup-content">
        {items.length === 0 && <div className="empty">该分区没有右键菜单项</div>}

        <div className="startup-section-title">已启用（{enabled.length}）</div>
        {enabled.map(renderRow)}

        {disabled.length > 0 && (
          <>
            <button
              className="startup-section-title startup-section-toggle"
              onClick={() => setShowDisabled((v) => !v)}
            >
              已禁用（{disabled.length}）
              <span className="startup-arrow">{showDisabled ? '▾' : '▸'}</span>
            </button>
            {showDisabled && disabled.map(renderRow)}
          </>
        )}
      </main>

      {dialog && (
        <div className="modal-overlay" onClick={() => setDialog(null)}>
          <div className="modal modal-form" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <span className="modal-title">{dialog.item ? '编辑菜单项' : '新增菜单项'}</span>
              <button className="modal-close" aria-label="关闭" onClick={() => setDialog(null)}>
                <CloseIcon />
              </button>
            </div>
            <div className="modal-body form-body">
              <label className="form-row">
                <span>名称</span>
                <input
                  value={dName}
                  onChange={(e) => setDName(e.target.value)}
                  placeholder="显示在右键菜单中的名称"
                  autoFocus
                />
              </label>
              <label className="form-row">
                <span>命令</span>
                <input
                  value={dCommand}
                  onChange={(e) => setDCommand(e.target.value)}
                  placeholder='如 notepad.exe 或 "C:\Tools\app.exe" "%1"'
                />
              </label>
              {formErr && <div className="form-error">{formErr}</div>}
            </div>
            <div className="modal-footer">
              <div className="spacer" />
              <button className="btn-plain" onClick={() => setDialog(null)}>
                取消
              </button>
              <button className="btn-primary" onClick={save}>
                保存
              </button>
            </div>
          </div>
        </div>
      )}

      {error && (
        <div className="toast" role="status">
          <span>{error}</span>
        </div>
      )}
    </div>
  )
}

export default ContextMenuManager
