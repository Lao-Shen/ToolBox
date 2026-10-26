package main

import (
	"embed"
	"log"
	"os"
	"strings"
	"time"

	"changeme/core"
	"changeme/service"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/public/wails.png
var trayIcon []byte // 托盘图标（占位：暂无正式 logo）

// quitting 置真后不再拦截窗口关闭，保证退出流程能真正销毁窗口
var quitting bool

func init() {
	// Register a custom event whose associated data type is string.
	// This is not required, but the binding generator will pick up registered events
	// and provide a strongly typed JS/TS API for them.
	application.RegisterEvent[string]("time")
	// 快捷键触发未接入后端的工具时通知前端
	application.RegisterEvent[string]("hotkey")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	settingsService := &service.SettingsService{}
	screenshotService := &service.ScreenshotService{}
	hotkeyService := &service.HotkeyService{}
	startupService := &service.StartupService{}
	contextMenuService := &service.ContextMenuService{}
	iconExtractService := &service.IconExtractService{}
	updateService := NewUpdateService()

	// 先声明 mainWindow，供单例回调闭包引用（回调在二次启动时才执行，彼时窗口已创建）
	var mainWindow *application.WebviewWindow

	// 由开机自启拉起（注册表命令带 --autostart）时，主窗口创建即隐藏到托盘
	autoStarted := false
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, core.AutoStartArg) {
			autoStarted = true
			break
		}
	}

	app := application.New(application.Options{
		Name:        "ToolBox",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(startupService),
			application.NewService(settingsService),
			application.NewService(screenshotService),
			application.NewService(&service.ToolService{}),
			application.NewService(hotkeyService),
			application.NewService(contextMenuService),
			application.NewService(iconExtractService),
			application.NewService(updateService), // 应用更新
		},
		// 单例进程：二次启动时框架自动退出第二实例，这里聚焦既有主窗口（可能藏在托盘）
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "earwind.top.toolbox",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				mainWindow.Show()
				mainWindow.Focus()
			},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			// /icons/ 请求映射到数据目录（图标复制存档，源文件删除后仍可用）
			Middleware: service.IconAssetsMiddleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// 更新器：自研更新链路（清单 → 下载 → 校验 → 替换重启），启动静默检查
	setupUpdate(app, updateService)

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "工具箱",
		// 开机自启拉起时直接隐藏到托盘，不弹主界面
		Hidden: autoStarted,
		// 去掉 Windows 原生标题栏，保留圆角与阴影（DisableFramelessWindowDecorations 默认 false）
		Frameless: true,
		Windows: application.WindowsWindow{
			// 启用 WebView2 非客户区命中测试，否则前端 app-region: drag 拖动区域不生效
			NonClientRegionSupport: true,
		},
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1000,
		Height: 618,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})
	screenshotService.AttachWindow(mainWindow)
	service.ConfigureHotkeyActions(hotkeyService, screenshotService, startupService, contextMenuService, iconExtractService)

	// 兼容旧版写入的自启项：已开启自启时补上 --autostart，下次开机即隐藏启动
	go func() {
		if err := startupService.RefreshAutoStartEntry(); err != nil {
			log.Printf("刷新开机自启项失败: %v", err)
		}
	}()

	// 系统托盘：左键点击切换窗口显示/隐藏，右键弹出菜单
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("工具箱")
	trayMenu := application.NewMenu()
	trayMenu.Add("显示主界面").OnClick(func(ctx *application.Context) {
		mainWindow.Show().Focus()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
		quitting = true
		app.Quit()
	})
	tray.SetMenu(trayMenu)
	tray.AttachWindow(mainWindow)
	tray.Run()

	// 关闭窗口：hideToTray 开启时隐藏到托盘，否则正常关闭退出
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quitting {
			return // 退出流程：放行，走默认销毁
		}
		if !settingsService.HideToTray() {
			return // 开关关闭：正常关闭
		}
		mainWindow.Hide()
		event.Cancel()
	})

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
