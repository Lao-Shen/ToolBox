import { useEffect, useState } from 'react'
import { Group } from './types'
import { CloseIcon } from './icons'

interface GroupDialogProps {
  groups: Group[]
  onChange: (groups: Group[]) => void
  onClose: () => void
}

// 生成新分组 id（占位实现，接入后端后由后端生成）
function newGroupId(): string {
  return 'g' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6)
}

function ChevronUpIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <polyline points="18 15 12 9 6 15" />
    </svg>
  )
}

function ChevronDownIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <polyline points="6 9 12 15 18 9" />
    </svg>
  )
}

function EditIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
    </svg>
  )
}

function TrashIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <polyline points="3 6 5 6 21 6" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
    </svg>
  )
}

// 分组管理弹窗：分组列表，可多选、排序、重命名、新增、删除
function GroupDialog({ groups, onChange, onClose }: GroupDialogProps) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editingName, setEditingName] = useState('')
  // 待删除确认：选中组有工具时先提示再删
  const [pendingDelete, setPendingDelete] = useState<string[] | null>(null)

  // ESC 关闭弹窗
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const toggleSelect = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  // 上移/下移一行
  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= groups.length) return
    const next = [...groups]
    ;[next[index], next[target]] = [next[target], next[index]]
    onChange(next)
  }

  // 进入/提交重命名
  const startRename = (g: Group) => {
    setEditingId(g.id)
    setEditingName(g.name)
  }
  const commitRename = () => {
    if (!editingId) return
    const name = editingName.trim()
    if (name) {
      onChange(groups.map((g) => (g.id === editingId ? { ...g, name } : g)))
    }
    setEditingId(null)
  }

  // 新增分组：追加到末尾并进入重命名状态
  const addGroup = () => {
    const id = newGroupId()
    onChange([...groups, { id, name: '新分组', tools: [] }])
    setEditingId(id)
    setEditingName('新分组')
  }

  // 删除：组内有工具时先弹确认，无工具直接删
  const requestDelete = (ids: string[]) => {
    const hasTools = groups.some((g) => ids.includes(g.id) && g.tools.length > 0)
    if (hasTools) {
      setPendingDelete(ids)
    } else {
      doDelete(ids)
    }
  }

  const doDelete = (ids: string[]) => {
    onChange(groups.filter((g) => !ids.includes(g.id)))
    setSelected((prev) => {
      const next = new Set(prev)
      ids.forEach((id) => next.delete(id))
      return next
    })
    setPendingDelete(null)
  }

  // 确认删除提示文案（按当前分组数据计算）
  const pendingGroups = pendingDelete ? groups.filter((g) => pendingDelete.includes(g.id)) : []
  const pendingToolCount = pendingGroups.reduce((n, g) => n + g.tools.length, 0)

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">分组管理</span>
          <button className="modal-close" aria-label="关闭" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>

        <div className="modal-body">
          {groups.length === 0 && <div className="empty">暂无分组</div>}
          {groups.map((g, i) => (
            <div className={`group-row${selected.has(g.id) ? ' is-selected' : ''}`} key={g.id}>
              <label className="row-check">
                <input
                  type="checkbox"
                  checked={selected.has(g.id)}
                  onChange={() => toggleSelect(g.id)}
                />
              </label>
              {editingId === g.id ? (
                <input
                  className="row-rename-input"
                  value={editingName}
                  onChange={(e) => setEditingName(e.target.value)}
                  onBlur={commitRename}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') commitRename()
                    if (e.key === 'Escape') setEditingId(null)
                  }}
                  autoFocus
                />
              ) : (
                <span className="row-name" title={g.name}>{g.name}</span>
              )}
              <span className="row-count">{g.tools.length} 个工具</span>
              <div className="row-actions">
                <button className="row-btn" title="上移" disabled={i === 0} onClick={() => move(i, -1)}>
                  <ChevronUpIcon />
                </button>
                <button className="row-btn" title="下移" disabled={i === groups.length - 1} onClick={() => move(i, 1)}>
                  <ChevronDownIcon />
                </button>
                <button className="row-btn" title="重命名" onClick={() => startRename(g)}>
                  <EditIcon />
                </button>
                <button className="row-btn row-btn-danger" title="删除" onClick={() => requestDelete([g.id])}>
                  <TrashIcon />
                </button>
              </div>
            </div>
          ))}
        </div>

        {pendingDelete && (
          <div className="confirm-strip">
            <span>
              {pendingGroups.length === 1
                ? `“${pendingGroups[0]?.name}”内有 ${pendingToolCount} 个工具，删除分组后这些工具将一并移除。`
                : `所选 ${pendingGroups.length} 个分组内共有 ${pendingToolCount} 个工具，删除分组后这些工具将一并移除。`}
            </span>
            <button className="confirm-btn confirm-btn-danger" onClick={() => doDelete(pendingDelete)}>
              确认删除
            </button>
            <button className="confirm-btn" onClick={() => setPendingDelete(null)}>
              取消
            </button>
          </div>
        )}

        <div className="modal-footer">
          <button className="btn-plain" onClick={addGroup}>＋ 新增分组</button>
          <div className="spacer" />
          <button
            className="btn-plain"
            disabled={selected.size === 0}
            onClick={() => requestDelete([...selected])}
          >
            删除选中{selected.size > 0 ? `（${selected.size}）` : ''}
          </button>
          <button className="btn-primary" onClick={onClose}>完成</button>
        </div>
      </div>
    </div>
  )
}

export default GroupDialog
