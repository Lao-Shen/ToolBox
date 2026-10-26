import { useEffect, useState } from 'react'
import { CloseIcon } from './icons'

interface ShortcutDialogProps {
  current: string // 当前快捷键（空 = 无）
  onSave: (shortcut: string) => void
  onClose: () => void
}

// 修饰键本身不构成快捷键，等主键一起组合
const MODIFIERS = ['Control', 'Alt', 'Shift', 'Meta']

// 按键显示名归一化
function normalizeKey(key: string): string {
  if (key.length === 1) return key.toUpperCase()
  if (key === ' ') return 'Space'
  const names: Record<string, string> = {
    Escape: 'Esc',
    ArrowUp: '↑',
    ArrowDown: '↓',
    ArrowLeft: '←',
    ArrowRight: '→',
  }
  return names[key] || key // F1-F12、Enter 等原样
}

// 快捷键分配弹窗：按下组合键即捕获
function ShortcutDialog({ current, onSave, onClose }: ShortcutDialogProps) {
  const [capture, setCapture] = useState('')

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      e.preventDefault()
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (MODIFIERS.includes(e.key)) return // 只按修饰键不响应
      const parts: string[] = []
      if (e.ctrlKey) parts.push('Ctrl')
      if (e.altKey) parts.push('Alt')
      if (e.shiftKey) parts.push('Shift')
      if (e.metaKey) parts.push('Win')
      parts.push(normalizeKey(e.key))
      if (parts.length < 2) return // 无修饰键的裸按键不分配
      setCapture(parts.join('+'))
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal modal-form" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">设置快捷键</span>
          <button className="modal-close" aria-label="关闭" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>
        <div className="modal-body form-body">
          <div className="form-row">
            <span>当前</span>
            <span className="setting-value">{current || '无'}</span>
          </div>
          <div className="capture-box">
            {capture ? (
              <kbd className="capture-key">{capture}</kbd>
            ) : (
              <span className="capture-hint">按下新快捷键组合…（如 Ctrl+Alt+S）</span>
            )}
          </div>
        </div>
        <div className="modal-footer">
          <button className="btn-plain" onClick={() => setCapture('')}>清空输入</button>
          <div className="spacer" />
          <button className="btn-plain" onClick={() => onSave('')}>清除快捷键</button>
          <button className="btn-primary" disabled={!capture} onClick={() => onSave(capture)}>保存</button>
        </div>
      </div>
    </div>
  )
}

export default ShortcutDialog
