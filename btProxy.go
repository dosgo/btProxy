package main

import (
	"dosgo/btProxy/comm"
	"dosgo/btProxy/icon"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// AppUI 管理应用程序界面
type AppUI struct {
	app    fyne.App
	window fyne.Window
	config *comm.Config

	// UI 组件
	macEntry  *widget.Entry
	portEntry *widget.Entry
	autoStart *widget.Check
	startBtn  *widget.Button
	hideBtn   *widget.Button

	// 托盘
	systray  fyne.App
	quitMenu *fyne.MenuItem
	showMenu *fyne.MenuItem
}

// NewAppUI 创建新的应用程序实例
func NewAppUI() *AppUI {
	a := app.NewWithID("com.bluetooth.proxy")
	w := a.NewWindow("蓝牙代理工具")
	w.SetIcon(resourceIconPng) // 需要添加图标资源
	a.SetIcon(resourceIconPng)
	// 默认配置

	return &AppUI{
		app:    a,
		window: w,
		config: comm.LoadConfig(),
	}
}

var resourceIconPng = &fyne.StaticResource{
	StaticName:    "logo.png",
	StaticContent: icon.IconDataPng, //
}

// createUI 创建用户界面
func (ui *AppUI) createUI() fyne.CanvasObject {

	// 蓝牙 MAC 地址输入框
	ui.macEntry = widget.NewEntry()
	ui.macEntry.SetPlaceHolder("请输入蓝牙 MAC 地址 (例如: 00:11:22:33:44:55)")
	ui.macEntry.SetText(ui.config.BluetoothMAC)
	ui.macEntry.Validator = func(s string) error {
		// 简单的 MAC 地址验证
		if s == "" {
			return fmt.Errorf("MAC 地址不能为空")
		}
		// 移除分隔符并验证长度
		clean := strings.ReplaceAll(s, ":", "")
		clean = strings.ReplaceAll(clean, "-", "")
		if len(clean) != 12 {
			return fmt.Errorf("MAC 地址应为 12 位十六进制字符")
		}
		return nil
	}
	macForm := &widget.FormItem{
		Text:   "蓝牙 MAC 地址:",
		Widget: ui.macEntry,
	}

	// 端口号输入框
	ui.portEntry = widget.NewEntry()
	ui.portEntry.SetPlaceHolder("请输入端口号 (1024-65535)")
	ui.portEntry.SetText(strconv.Itoa(ui.config.Port))
	ui.portEntry.Validator = func(s string) error {
		port, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("端口号必须是数字")
		}
		if port < 1024 || port > 65535 {
			return fmt.Errorf("端口号必须在 1024-65535 之间")
		}
		return nil
	}
	portForm := &widget.FormItem{
		Text:   "监听端口号:",
		Widget: ui.portEntry,
	}

	// 自动启动复选框
	ui.autoStart = widget.NewCheck("自动启动服务", func(checked bool) {
		ui.config.AutoStart = checked
		comm.SaveConfig(ui.config)
	})
	ui.autoStart.SetChecked(ui.config.AutoStart)

	// 启动按钮
	ui.startBtn = widget.NewButton("启动代理", func() {
		if ui.startBtn.Text == "停止代理" {
			ui.stopProxy()
			return
		}
		if err := ui.startProxy(); err != nil {
			dialog.ShowError(err, ui.window)
		} else {
			dialog.ShowInformation("成功", "蓝牙代理已启动", ui.window)
		}
	})
	ui.startBtn.Importance = widget.HighImportance

	// 隐藏窗口按钮
	ui.hideBtn = widget.NewButton("隐藏到系统托盘", func() {
		ui.hideToTray()
	})

	// 状态栏
	statusLabel := widget.NewLabel("就绪")
	statusBar := container.NewHBox(
		layout.NewSpacer(),
		statusLabel,
	)

	// 布局
	form := container.NewVBox(
		widget.NewSeparator(),
		container.New(layout.NewGridWrapLayout(fyne.NewSize(300, 180)), widget.NewForm(macForm, portForm)),
		ui.autoStart,
		layout.NewSpacer(),
		container.NewHBox(
			ui.startBtn,
			layout.NewSpacer(),
			ui.hideBtn,
		),
		widget.NewSeparator(),
		statusBar,
	)

	return container.NewPadded(form)
}

func (ui *AppUI) stopProxy() error {
	comm.StopProxy()
	ui.startBtn.Text = "启动代理"
	return nil
}

// startProxy 启动蓝牙代理
func (ui *AppUI) startProxy() error {
	// 验证输入
	if err := ui.macEntry.Validate(); err != nil {
		return fmt.Errorf("MAC 地址无效: %v", err)
	}
	if err := ui.portEntry.Validate(); err != nil {
		return fmt.Errorf("端口号无效: %v", err)
	}

	// 保存配置
	port, _ := strconv.Atoi(ui.portEntry.Text)
	ui.config.BluetoothMAC = ui.macEntry.Text
	ui.config.Port = port
	comm.SaveConfig(ui.config)

	btRaw := comm.NewConnectBT(ui.macEntry.Text)
	//多路复用
	mux := comm.NewMuxManager(btRaw)
	go comm.StartProxy(mux, fmt.Sprintf(":%d", port), "127.0.0.1:8023")

	fmt.Printf("启动蓝牙代理: MAC=%s, Port=%d, AutoStart=%v\n",
		ui.config.BluetoothMAC, ui.config.Port, ui.config.AutoStart)
	ui.startBtn.Text = "停止代理"
	return nil
}

// hideToTray 隐藏窗口到系统托盘
func (ui *AppUI) hideToTray() {
	if desk, ok := ui.app.(desktop.App); ok {
		ui.setupTray(desk)
		ui.window.Hide()
	} else {
		dialog.ShowInformation("提示", "当前平台不支持系统托盘", ui.window)
	}
}

// setupTray 设置系统托盘
func (ui *AppUI) setupTray(desk desktop.App) {
	// 创建托盘菜单
	ui.showMenu = fyne.NewMenuItem("显示窗口", func() {
		ui.window.Show()
		ui.showMenu.Disabled = true
	})
	ui.quitMenu = fyne.NewMenuItem("退出", func() {
		ui.app.Quit()
	})

	// 如果已经存在托盘菜单，先移除
	desk.SetSystemTrayMenu(fyne.NewMenu("蓝牙代理",
		ui.showMenu,
		fyne.NewMenuItemSeparator(),
		ui.quitMenu,
	))
}

// showWindow 显示窗口
func (ui *AppUI) showWindow() {
	ui.window.Show()
	if ui.showMenu != nil {
		ui.showMenu.Disabled = true
	}
}

// Run 运行应用程序
func (ui *AppUI) Run() {
	// 设置窗口
	ui.window.SetContent(ui.createUI())
	ui.window.Resize(fyne.NewSize(400, 300))
	ui.window.SetMaster()
	ui.window.CenterOnScreen()

	// 设置托盘（如果支持）
	if desk, ok := ui.app.(desktop.App); ok {
		ui.setupTray(desk)
		// 窗口关闭时最小化到托盘
		ui.window.SetCloseIntercept(func() {
			ui.window.Hide()
		})
	}
	//自动启动
	if ui.config.AutoStart {
		ui.startProxy()
	}
	ui.window.ShowAndRun()
}

func main() {
	// 创建应用实例
	ui := NewAppUI()
	// 运行应用
	ui.Run()
}
