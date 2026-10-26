import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { Settings } from './types'
import { CloseIcon } from './icons'
import * as Update from '../bindings/changeme/updateservice'

interface SettingsDialogProps {
  settings: Settings
  onChange: (settings: Settings) => void
  onClose: () => void
  /** 打开时自动检查一次更新（启动静默检查发现新版时用） */
  autoCheck?: boolean
}

// 设置类别：通用 / 关于
const CATEGORIES = ['通用', '关于'] as const
type Category = (typeof CATEGORIES)[number]

// 设置弹窗：左侧类别导航，右侧类别内容
function SettingsDialog({ settings, onChange, onClose, autoCheck = false }: SettingsDialogProps) {
  const [category, setCategory] = useState<Category>('通用')
  // 更新模块状态
  const [version, setVersion] = useState('')
  const [autoUpdate, setAutoUpdate] = useState(true)
  const [checking, setChecking] = useState(false)
  const [updateVersion, setUpdateVersion] = useState('')
  const [updateNotes, setUpdateNotes] = useState('')
  const [updateStatus, setUpdateStatus] = useState('')
  const [updateReady, setUpdateReady] = useState(false)
  const [updatePercent, setUpdatePercent] = useState(0)
  const [docStatus, setDocStatus] = useState('')

  // 打开时读取版本号与自动更新开关
  useEffect(() => {
    Update.CurrentVersion().then(setVersion).catch(() => {})
    Update.AutoUpdateEnabled().then(setAutoUpdate).catch(() => {})
  }, [])

  // 恢复更新状态（启动自动下载可能已在后台进行/完成），并订阅下载进度
  useEffect(() => {
    Update.GetUpdateState()
      .then((st: any) => {
        if (!st) return
        if (st.status === 'downloading') {
          setUpdatePercent(st.percent || 0)
          setUpdateStatus(`正在下载更新… ${st.percent || 0}%`)
        } else if (st.status === 'ready') {
          setUpdateReady(true)
          setUpdatePercent(100)
          setUpdateVersion(st.version || '')
          setUpdateNotes(st.notes || '')
          setUpdateStatus(`新版本 ${st.version} 已下载，重启后生效`)
        } else if (st.status === 'error' && st.error) {
          setUpdateStatus('下载更新失败：' + st.error)
        }
      })
      .catch(() => {})

    const off = Events.On('app:update:progress', (ev: any) => {
      const p = ev?.data
      if (!p) return
      if (p.status === 'downloading') {
        setUpdatePercent(p.percent || 0)
        setUpdateStatus(`正在下载更新… ${p.percent || 0}%`)
      } else if (p.status === 'ready') {
        setUpdateReady(true)
        setUpdatePercent(100)
        setUpdateVersion(p.version || '')
        setUpdateStatus(`新版本 ${p.version} 已下载，重启后生效`)
      } else if (p.status === 'error') {
        setUpdateStatus('下载更新失败：' + (p.error || '未知错误'))
      }
    })
    return () => off()
  }, [])

  // ESC 关闭弹窗
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const set = (patch: Partial<Settings>) => onChange({ ...settings, ...patch })

  const toggleAutoUpdate = async (v: boolean) => {
    setAutoUpdate(v)
    try {
      await Update.SetAutoUpdateEnabled(v)
    } catch (e) {
      setAutoUpdate(!v)
      setUpdateStatus('保存失败：' + String(e))
    }
  }

  const checkUpdate = async () => {
    setChecking(true)
    setUpdateStatus('')
    setUpdateVersion('')
    setUpdateNotes('')
    setUpdateReady(false)
    setUpdatePercent(0)
    try {
      const info: any = await Update.CheckUpdate()
      if (info?.available) {
        setUpdateVersion(info.version)
        setUpdateNotes(info.notes || '')
        if (info.ready) {
          setUpdateReady(true)
          setUpdatePercent(100)
          setUpdateStatus(`新版本 ${info.version} 已下载，重启后生效`)
        } else {
          setUpdateStatus(`发现新版本 ${info.version}`)
        }
      } else {
        setUpdateStatus('已是最新版本')
      }
    } catch (e) {
      setUpdateStatus('检查更新失败：' + String(e))
    } finally {
      setChecking(false)
    }
  }

  // 启动静默检查发现新版 → 打开即自动检查
  useEffect(() => {
    if (autoCheck) void checkUpdate()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoCheck])

  const install = async () => {
    try {
      if (updateReady) {
        setUpdateStatus('正在重启并安装…')
        await Update.ApplyUpdate()
      } else {
        await Update.StartDownload()
        setUpdateStatus('正在下载更新…')
      }
    } catch (e) {
      setUpdateStatus('更新失败：' + String(e))
    }
  }

  const openDoc = () => {
    setDocStatus('文档整理中，敬请期待')
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal modal-settings" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">设置</span>
          <button className="modal-close" aria-label="关闭" onClick={onClose}>
            <CloseIcon />
          </button>
        </div>

        {/* 左侧：设置项类别 */}
        <div className="settings-body">
          <nav className="settings-nav">
            {CATEGORIES.map((c) => (
              <button
                key={c}
                className={`settings-nav-item${c === category ? ' is-active' : ''}`}
                onClick={() => setCategory(c)}
              >
                {c}
              </button>
            ))}
          </nav>

          {/* 右侧：类别内容 */}
          <div className="settings-content">
          {category === '通用' && (
            <>
              <div className="setting-row">
                <span className="setting-label">开机自启</span>
                <label className="toggle">
                  <input
                    type="checkbox"
                    checked={settings.autoStart}
                    onChange={(e) => set({ autoStart: e.target.checked })}
                  />
                  <span className="slider" />
                </label>
              </div>
              <div className="setting-row">
                <span className="setting-label">关闭时隐藏到托盘</span>
                <label className="toggle">
                  <input
                    type="checkbox"
                    checked={settings.hideToTray}
                    onChange={(e) => set({ hideToTray: e.target.checked })}
                  />
                  <span className="slider" />
                </label>
              </div>
              <div className="setting-row">
                <span className="setting-label">自动更新（启动时检查）</span>
                <label className="toggle">
                  <input
                    type="checkbox"
                    checked={autoUpdate}
                    onChange={(e) => void toggleAutoUpdate(e.target.checked)}
                  />
                  <span className="slider" />
                </label>
              </div>
            </>
          )}

          {category === '关于' && (
            <>
              <div className="setting-row">
                <span className="setting-label">版本号</span>
                <span className="setting-value">{version || '—'}</span>
              </div>
              <div className="setting-row">
                <span className="setting-label">检查更新</span>
                <button className="btn-plain" onClick={() => void checkUpdate()} disabled={checking}>
                  {checking ? '检查中…' : '检查更新'}
                </button>
              </div>
              {updateVersion && (
                <div className="setting-row">
                  <span className="setting-label">
                    新版本 {updateVersion}
                    {!updateReady && updatePercent > 0 ? `（${updatePercent}%）` : ''}
                  </span>
                  <button className="btn-plain" onClick={() => void install()}>
                    {updateReady ? '重启并安装' : '下载并安装'}
                  </button>
                </div>
              )}
              {updateNotes && (
                <div className="setting-status" style={{ whiteSpace: 'pre-line' }}>{updateNotes}</div>
              )}
              {updateStatus && <div className="setting-status">{updateStatus}</div>}
              <div className="setting-row">
                <span className="setting-label">文档</span>
                <button className="btn-plain" onClick={openDoc}>打开文档</button>
              </div>
              {docStatus && <div className="setting-status">{docStatus}</div>}
            </>
          )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default SettingsDialog
