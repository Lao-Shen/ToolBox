// 工具卡片字段：图标、名称、描述、快捷键、工作路径
export interface Tool {
  icon: string // 图标
  name: string // 名称
  description: string // 描述
  shortcut: string // 快捷键
  workdir: string // 工作路径
  action?: string // 点击行为标识（后端分发），占位实现
}

// 分组：左侧分组列表中的一项
export interface Group {
  id: string // 稳定标识，重命名/排序后不变化
  name: string
  tools: Tool[]
}

// 应用设置项
export interface Settings {
  autoStart: boolean // 开机自启
  hideToTray: boolean // 关闭时隐藏到托盘
}

// 是否图片图标（数据目录路径或 data URL；否则为 emoji/字符）
export const isImageIcon = (icon: string): boolean =>
  icon.startsWith('/icons/') || icon.startsWith('data:image')

// 应用版本号
export const APP_VERSION = 'v0.1.0'

