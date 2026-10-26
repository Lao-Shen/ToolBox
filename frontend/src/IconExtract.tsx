import { useRef, useState } from 'react'
import { IconExtractService, ToolService } from '../bindings/changeme/service'

// 图标提取页面（独立窗口，?page=iconextract 路由）
// 选择 exe/dll → 预览主图标（256×256）→ 确认导出 PNG 到所选目录
function IconExtract() {
  const [exePath, setExePath] = useState('')
  const [preview, setPreview] = useState('')
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState('')
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const show = (message: string) => {
    setToast(message)
    clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(''), 4000)
  }

  const pickExe = () => {
    IconExtractService.PickExe()
      .then((p) => {
        if (!p) return // 用户取消
        setExePath(p)
        setPreview('')
        setBusy(true)
        IconExtractService.Preview(p)
          .then((dataUrl) => setPreview(dataUrl || ''))
          .catch((e) => show(`提取失败：${e}`))
          .finally(() => setBusy(false))
      })
      .catch((e) => show(`选择失败：${e}`))
  }

  const exportIcon = () => {
    ToolService.PickDirectory()
      .then((dir) => {
        if (!dir) return // 用户取消
        setBusy(true)
        IconExtractService.Export(exePath, dir)
          .then((p) => show(`已导出：${p}`))
          .catch((e) => show(`导出失败：${e}`))
          .finally(() => setBusy(false))
      })
      .catch((e) => show(String(e)))
  }

  return (
    <div className="app">
      <header className="header">
        <span className="header-title">图标提取</span>
      </header>

      <main className="content ie-body">
        <div className="ie-preview">
          {preview ? (
            <img src={preview} alt="图标预览" />
          ) : (
            <span className="ie-empty">选择 exe 后显示图标预览</span>
          )}
        </div>
        <div className="ie-path" title={exePath}>
          {exePath || '未选择文件'}
        </div>
        <div className="ie-actions">
          <button className="btn-plain" onClick={pickExe} disabled={busy}>
            选择 exe…
          </button>
          <button className="btn-primary" onClick={exportIcon} disabled={!exePath || busy}>
            确认导出
          </button>
        </div>
      </main>

      {toast && (
        <div className="toast" role="status">
          <span>{toast}</span>
        </div>
      )}
    </div>
  )
}

export default IconExtract
