package service

import "testing"

func TestAutoStartExe(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{`"C:\Program Files\ToolBox\ToolBox.exe" --autostart`, `C:\Program Files\ToolBox\ToolBox.exe`},
		{`"D:\MySoftware\ToolBox\bin\ToolBox.exe"`, `D:\MySoftware\ToolBox\bin\ToolBox.exe`},
		{`D:\MySoftware\ToolBox\bin\ToolBox.exe`, `D:\MySoftware\ToolBox\bin\ToolBox.exe`},
		{`D:\MySoftware\ToolBox\bin\ToolBox.exe --autostart`, `D:\MySoftware\ToolBox\bin\ToolBox.exe`},
	}
	for _, c := range cases {
		if got := autoStartExe(c.cmd); got != c.want {
			t.Errorf("autoStartExe(%q)=%q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestSameWindowsPath(t *testing.T) {
	if !sameWindowsPath(`D:\MySoftware\ToolBox\bin\ToolBox.exe`, `d:\mysoftware\toolbox\bin\toolbox.exe`) {
		t.Error("Windows 路径应忽略大小写")
	}
	if sameWindowsPath(`D:\A\ToolBox.exe`, `D:\B\ToolBox.exe`) {
		t.Error("不同路径不应相等")
	}
	if sameWindowsPath("", `D:\A\ToolBox.exe`) {
		t.Error("空路径不应匹配")
	}
}
