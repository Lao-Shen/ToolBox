import { useEffect, useState } from 'react'
import { ToolService } from '../bindings/changeme/service'
import { Tool } from './types'
import { CloseIcon } from './icons'

interface WorkdirDialogProps {
  tool: Tool
  onSave: (workdir: string) => void
  onClose: () => void
}

// 工作目录弹窗：目录选择器选目录（可手动编辑）→ 确认
function WorkdirDialog({ tool, onSave, onClose }: WorkdirDialogProps) {
  const [workdir, setWorkdir] = useState(tool.workdir)
  const [err, setErr] = useState('')

  // ESC 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const pick = () => {
    setErr('')
    ToolService.PickDirectory()
      .then((dir) => {
        if (dir) setWorkdir(dir)
      })
      .catch((e) => setErr(String(e)))
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal modal-form" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">工作目录</span>
          <button className="modal-close" aria-label="关闭" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>
        <div className="modal-body form-body">
          <label className="form-row">
            <span>路径</span>
            <input
              value={workdir}
              onChange={(e) => setWorkdir(e.target.value)}
              placeholder="截图等工具的保存位置"
            />
          </label>
          <div className="form-row">
            <span />
            <div className="icon-pick">
              <button className="btn-plain" onClick={pick}>
                选择目录…
              </button>
            </div>
          </div>
          {err && <div className="form-error">{err}</div>}
        </div>
        <div className="modal-footer">
          <div className="spacer" />
          <button className="btn-plain" onClick={onClose}>
            取消
          </button>
          <button className="btn-primary" onClick={() => onSave(workdir)}>
            确认
          </button>
        </div>
      </div>
    </div>
  )
}

export default WorkdirDialog
