import { useEffect, useState } from 'react'
import { ToolService } from '../bindings/changeme/service'
import { Tool, isImageIcon } from './types'
import { CloseIcon } from './icons'

interface ToolEditDialogProps {
  tool: Tool
  onSave: (tool: Tool) => void
  onClose: () => void
}

// 修改工具信息弹窗：图标（文件选择/emoji）、名称、描述、工作路径
function ToolEditDialog({ tool, onSave, onClose }: ToolEditDialogProps) {
  const [icon, setIcon] = useState(tool.icon)
  const [name, setName] = useState(tool.name)
  const [description, setDescription] = useState(tool.description)
  const [workdir, setWorkdir] = useState(tool.workdir)
  const [pickError, setPickError] = useState('')

  const isImage = isImageIcon(icon)

  // ESC 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  // 从资源管理器选择图标（后端原生文件对话框 → data URL）
  const pickIcon = () => {
    setPickError('')
    ToolService.PickIcon()
      .then((dataUrl) => {
        if (dataUrl) setIcon(dataUrl)
      })
      .catch((e) => setPickError(String(e)))
  }

  const save = () => {
    if (!name.trim()) return
    onSave({ ...tool, icon: icon || '🛠', name: name.trim(), description, workdir })
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal modal-form" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">修改工具</span>
          <button className="modal-close" aria-label="关闭" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>
        <div className="modal-body form-body">
          <div className="form-row">
            <span>图标</span>
            <div className="icon-pick">
              {isImage ? (
                <img className="icon-preview" src={icon} alt="" />
              ) : (
                <span className="icon-preview icon-preview-emoji">{icon || '🛠'}</span>
              )}
              <input
                className="icon-emoji-input"
                value={isImage ? '' : icon}
                onChange={(e) => setIcon(e.target.value)}
                placeholder="emoji/字符"
              />
              <button className="btn-plain" onClick={pickIcon}>选择图标…</button>
            </div>
          </div>
          {pickError && <div className="form-error">{pickError}</div>}
          <label className="form-row">
            <span>名称</span>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="工具名称" />
          </label>
          <label className="form-row">
            <span>描述</span>
            <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="一句话描述" />
          </label>
          <label className="form-row">
            <span>工作路径</span>
            <input value={workdir} onChange={(e) => setWorkdir(e.target.value)} placeholder="如 E:\\Code\\ToolBox" />
          </label>
        </div>
        <div className="modal-footer">
          <div className="spacer" />
          <button className="btn-plain" onClick={onClose}>取消</button>
          <button className="btn-primary" onClick={save}>保存</button>
        </div>
      </div>
    </div>
  )
}

export default ToolEditDialog
