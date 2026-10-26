package core

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// 注册表操作

// 鼠标在不同位置右键菜单对应的注册表中的位置

const (
	AnyFile      = "HKEY_CLASSES_ROOT\\*\\shell\\"                     // 对任意文件右键
	Folder       = "HKEY_CLASSES_ROOT\\Folder\\shell\\"                // 对文件夹右键
	DesktopBlock = "HKEY_CLASSES_ROOT\\Directory\\Background\\shell\\" // 桌面和空白区右键
)

// classesRootPrefix 常量中带的根键名前缀，OpenKey 时需去掉
const classesRootPrefix = "HKEY_CLASSES_ROOT\\"

// 开机自启动对应的注册表位置（HKCU 与 HKLM 同路径，根键不同）

const StartupRun = `Software\Microsoft\Windows\CurrentVersion\Run` // 开机自启动 Run 键

// StartupValueName 本应用在 Run 键下的值名
const StartupValueName = "ToolBox"

// AutoStartArg 开机自启时附加的命令行参数：带此参数启动则隐藏主界面到托盘
const AutoStartArg = "--autostart"

type RightClickMenu struct {
	Key         string // 注册表键名
	DisplayName string // 显示名称（默认值）
	MUIVerb     string // 本地化显示名称，优先级高于 DisplayName
	Icon        string // 图标
	Command     string // 启动命令
	Extended    bool   // 存在此值时，shift + 右键 才会出现该菜单项
	Enabled     bool   // LegacyDisable 值存在 = 已禁用
	Source      string // 定义所在："HKCU"（当前用户，可改可删）/ "HKLM"（所有用户，只读）
}

// StartupItem 开机自启动项
type StartupItem struct {
	Name    string // 启动项名称（值名）
	Command string // 启动命令
	Source  string // 来源："HKCU"（当前用户）或 "HKLM"（所有用户）
}

// GetAnyFileMenu 查询对任意文件右键的菜单配置（HKCR\*\shell）
func GetAnyFileMenu() ([]RightClickMenu, error) {
	return getRightClickMenus(AnyFile)
}

// GetFolderMenu 查询对文件夹右键的菜单配置（HKCR\Folder\shell）
func GetFolderMenu() ([]RightClickMenu, error) {
	return getRightClickMenus(Folder)
}

// GetDesktopBlockMenu 查询桌面和空白区右键菜单配置（HKCR\Directory\Background\shell）
func GetDesktopBlockMenu() ([]RightClickMenu, error) {
	return getRightClickMenus(DesktopBlock)
}

// getRightClickMenus 读取指定 shell 位置下所有菜单项（HKCR 合并视图：HKCU 覆盖 + HKLM）
func getRightClickMenus(shellPath string) ([]RightClickMenu, error) {
	rel := strings.TrimPrefix(shellPath, classesRootPrefix)
	// READ 含 ENUMERATE_SUB_KEYS 权限，否则 RegEnumKeyEx 枚举子键时报 Access is denied
	shellKey, err := registry.OpenKey(registry.CLASSES_ROOT, rel, registry.READ)
	if err != nil {
		return nil, err
	}
	defer shellKey.Close()

	subKeyNames, err := shellKey.ReadSubKeyNames(-1) // -1 表示读取全部
	if err != nil {
		return nil, err
	}

	menus := make([]RightClickMenu, 0, len(subKeyNames))
	for _, subKeyName := range subKeyNames {
		menu, err := readMenu(shellKey, rel, subKeyName)
		if err != nil {
			return nil, err
		}
		menus = append(menus, menu)
	}
	return menus, nil
}

// readMenu 读取 shell 子键下的单个菜单项配置
func readMenu(shellKey registry.Key, rel, name string) (RightClickMenu, error) {
	menu := RightClickMenu{Key: name, Enabled: true}
	subKey, err := registry.OpenKey(shellKey, name, registry.QUERY_VALUE)
	if err != nil {
		return menu, err
	}
	defer subKey.Close()

	menu.DisplayName, _, _ = subKey.GetStringValue("")    // 默认值 = 显示名称
	menu.MUIVerb, _, _ = subKey.GetStringValue("MUIVerb") // 本地化显示名称，优先级高于默认值
	menu.Icon, _, _ = subKey.GetStringValue("Icon")       // 图标

	// Extended 值存在（值可为空）即 Shift 右键才显示
	if _, _, err := subKey.GetStringValue("Extended"); err == nil {
		menu.Extended = true
	}
	// LegacyDisable 值存在（值可为空）即已禁用
	if _, _, err := subKey.GetStringValue("LegacyDisable"); err == nil {
		menu.Enabled = false
	}

	// 启动命令：command 子键的默认值
	if commandKey, err := registry.OpenKey(subKey, `command`, registry.QUERY_VALUE); err == nil {
		menu.Command, _, _ = commandKey.GetStringValue("")
		commandKey.Close()
	}

	menu.Source = menuSource(rel, name)
	return menu, nil
}

// readStringValue 读取注册表路径（HKCR\path）的默认字符串值
func readStringValue(path string) (string, error) {
	key, err := registry.OpenKey(registry.CLASSES_ROOT, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()

	val, _, err := key.GetStringValue("")
	return val, err
}

// GetStartupItems 查询开机自启动项（HKCU 与 HKLM 的 Run 键）
func GetStartupItems() ([]StartupItem, error) {
	items := make([]StartupItem, 0)

	for _, root := range []struct {
		key    registry.Key
		source string
	}{
		{registry.CURRENT_USER, "HKCU"},  // 当前用户，免管理员权限即可读写
		{registry.LOCAL_MACHINE, "HKLM"}, // 所有用户
	} {
		runKey, err := registry.OpenKey(root.key, StartupRun, registry.READ)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue // Run 键不存在 = 无自启动项
			}
			return nil, err
		}

		names, err := runKey.ReadValueNames(-1) // -1 表示读取全部
		if err != nil {
			runKey.Close()
			return nil, err
		}

		for _, name := range names {
			command, _, _ := runKey.GetStringValue(name)
			items = append(items, StartupItem{Name: name, Command: command, Source: root.source})
		}
		runKey.Close()
	}
	return items, nil
}

// SetAutoStart 写入/删除开机自启动项（HKCU Run 键，免管理员权限）
func SetAutoStart(enabled bool) error {
	runKey, err := registry.OpenKey(registry.CURRENT_USER, StartupRun, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer runKey.Close()

	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		// 路径加引号（可能含空格），并附加 --autostart 让开机启动时隐藏界面
		return runKey.SetStringValue(StartupValueName, `"`+exe+`" `+AutoStartArg)
	}

	// 值不存在视为已关闭，不报错
	if err := runKey.DeleteValue(StartupValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// SetStartupItemEnabled 开启/关闭指定自启动项（source: "HKCU"/"HKLM"，操作 Run 键的对应值）
func SetStartupItemEnabled(source, name, command string, enabled bool) error {
	root := registry.CURRENT_USER
	if source == "HKLM" {
		root = registry.LOCAL_MACHINE
	}
	runKey, err := registry.OpenKey(root, StartupRun, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer runKey.Close()

	if enabled {
		if command == "" {
			return errors.New("缺少启动命令")
		}
		return runKey.SetStringValue(name, command)
	}
	// 值不存在视为已关闭，不报错
	if err := runKey.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// ---- 右键菜单管理写操作（统一写 HKCU\Software\Classes 覆盖键，免管理员） ----

// userClassesPrefix HKCU/HKLM 下类根路径（HKCR 合并视图 = 这两个键的并集）
const userClassesPrefix = `Software\Classes\`

// menuShellPath 分区名 → shell 相对路径（去掉根键前缀，与 HKCU\Software\Classes / HKLM\Software\Classes 拼接）
func menuShellPath(section string) (string, error) {
	switch section {
	case "file":
		return strings.TrimPrefix(AnyFile, classesRootPrefix), nil
	case "folder":
		return strings.TrimPrefix(Folder, classesRootPrefix), nil
	case "desktop":
		return strings.TrimPrefix(DesktopBlock, classesRootPrefix), nil
	}
	return "", fmt.Errorf("未知分区 %s", section)
}

// menuSource 菜单项定义所在：HKCU 键存在且有实际定义（默认值或 command 子键）才算当前用户项；
// 仅有 LegacyDisable 的覆盖键（禁用系统项时创建）不算，定义仍在 HKLM。
func menuSource(rel, name string) string {
	key, err := registry.OpenKey(registry.CURRENT_USER, userClassesPrefix+rel+name, registry.QUERY_VALUE)
	if err != nil {
		return "HKLM"
	}
	defer key.Close()

	if _, _, err := key.GetStringValue(""); err == nil {
		return "HKCU"
	}
	if commandKey, err := registry.OpenKey(key, `command`, registry.QUERY_VALUE); err == nil {
		commandKey.Close()
		return "HKCU"
	}
	return "HKLM"
}

// SetRightClickMenuEnabled 启用/禁用菜单项：LegacyDisable 写在当前用户覆盖键上，
// 系统项（HKLM）同样生效（合并视图下 Explorer 见 LegacyDisable 即隐藏），免管理员。
func SetRightClickMenuEnabled(section, key string, enabled bool) error {
	rel, err := menuShellPath(section)
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, userClassesPrefix+rel+key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if enabled {
		// 值不存在视为已启用，不报错
		if err := k.DeleteValue("LegacyDisable"); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	return k.SetStringValue("LegacyDisable", "")
}

// DeleteRightClickMenu 删除菜单项（仅允许当前用户项，系统项删除需管理员）
func DeleteRightClickMenu(section, key string) error {
	rel, err := menuShellPath(section)
	if err != nil {
		return err
	}
	if menuSource(rel, key) != "HKCU" {
		return errors.New("该项属于所有用户，删除需要管理员权限")
	}
	// DeleteKey 只能删空键：先删 command 子键，再删主键
	if err := registry.DeleteKey(registry.CURRENT_USER, userClassesPrefix+rel+key+`\command`); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, userClassesPrefix+rel+key); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// SaveRightClickMenu 新增（key 为空，自动生成键名）或修改（key 非空，仅当前用户项）菜单项
func SaveRightClickMenu(section, key, name, command string) error {
	rel, err := menuShellPath(section)
	if err != nil {
		return err
	}
	if key == "" {
		key = uniqueMenuKey(rel, name)
	} else if menuSource(rel, key) != "HKCU" {
		return errors.New("该项属于所有用户，修改需要管理员权限")
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, userClassesPrefix+rel+key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("", name); err != nil {
		return err
	}

	commandKey, _, err := registry.CreateKey(registry.CURRENT_USER, userClassesPrefix+rel+key+`\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer commandKey.Close()
	return commandKey.SetStringValue("", command)
}

// sanitizeMenuKey 名称 → 键名（去掉注册表键名不允许的字符；空则用占位名）
func sanitizeMenuKey(name string) string {
	key := strings.Map(func(r rune) rune {
		if r == '\\' || r == '/' {
			return ' '
		}
		return r
	}, strings.TrimSpace(name))
	if key == "" {
		return fmt.Sprintf("ToolBoxItem%d", time.Now().UnixNano())
	}
	return key
}

// menuKeyExists 键名在合并视图（HKCU 或 HKLM）是否已存在
func menuKeyExists(rel, name string) bool {
	if _, err := registry.OpenKey(registry.CURRENT_USER, userClassesPrefix+rel+name, registry.QUERY_VALUE); err == nil {
		return true
	}
	if _, err := registry.OpenKey(registry.LOCAL_MACHINE, userClassesPrefix+rel+name, registry.QUERY_VALUE); err == nil {
		return true
	}
	return false
}

// uniqueMenuKey 生成不冲突的键名（重名自动加 -2/-3 序号）
func uniqueMenuKey(rel, name string) string {
	base := sanitizeMenuKey(name)
	if !menuKeyExists(rel, base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !menuKeyExists(rel, candidate) {
			return candidate
		}
	}
}

// AutoStartEnabled 查询开机自启动是否已开启
func AutoStartEnabled() (bool, error) {
	runKey, err := registry.OpenKey(registry.CURRENT_USER, StartupRun, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer runKey.Close()

	_, _, err = runKey.GetStringValue(StartupValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// AutoStartCommand 读取当前开机自启命令（未设置返回空串）
func AutoStartCommand() (string, error) {
	runKey, err := registry.OpenKey(registry.CURRENT_USER, StartupRun, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer runKey.Close()

	cmd, _, err := runKey.GetStringValue(StartupValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return cmd, nil
}
