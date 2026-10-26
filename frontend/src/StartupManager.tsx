import { useEffect, useState } from 'react'
import { StartupService, type StartupEntry } from '../bindings/changeme/service'

// 开机自启管理页面（独立窗口，?page=startup 路由）
function StartupManager() {
  const [items, setItems] = useState<StartupEntry[]>([])
  const [error, setError] = useState('')
  const [showDisabled, setShowDisabled] = useState(true)

  const refresh = () => {
    StartupService.List()
      .then((list) => setItems(list || []))
      .catch((e) => setError(String(e)))
  }
  useEffect(refresh, [])

  const toggle = (it: StartupEntry, enabled: boolean) => {
    setError('')
    StartupService.SetEnabled(it.source, it.name, it.command, enabled)
      .then(refresh)
      .catch((e) => setError(`操作失败：${e}`))
  }

  const enabled = items.filter((it) => it.enabled)
  const disabled = items.filter((it) => !it.enabled)

  const renderRow = (it: StartupEntry) => (
    <div className={`startup-row${it.enabled ? '' : ' is-disabled'}`} key={`${it.source}-${it.name}`}>
      <div className="startup-main">
        <span className="startup-name" title={it.name}>{it.name}</span>
        <span className="startup-path" title={it.command}>{it.command}</span>
      </div>
      <span className={`startup-source${it.source === 'HKLM' ? ' is-hklm' : ''}`}>
        {it.source === 'HKCU' ? '当前用户' : '所有用户'}
      </span>
      <label className="toggle">
        <input
          type="checkbox"
          checked={it.enabled}
          onChange={(e) => toggle(it, e.target.checked)}
        />
        <span className="slider" />
      </label>
    </div>
  )

  return (
    <div className="app">
      <header className="header">
        <span className="header-title">开机自启管理</span>
      </header>

      <main className="content startup-content">
        {items.length === 0 && <div className="empty">没有自启动项</div>}

        <div className="startup-section-title">已启用（{enabled.length}）</div>
        {enabled.map(renderRow)}

        {disabled.length > 0 && (
          <>
            <button
              className="startup-section-title startup-section-toggle"
              onClick={() => setShowDisabled((v) => !v)}
            >
              已关闭（{disabled.length}）
              <span className="startup-arrow">{showDisabled ? '▾' : '▸'}</span>
            </button>
            {showDisabled && disabled.map(renderRow)}
          </>
        )}
      </main>

      {error && (
        <div className="toast" role="status">
          <span>{error}</span>
        </div>
      )}
    </div>
  )
}

export default StartupManager
