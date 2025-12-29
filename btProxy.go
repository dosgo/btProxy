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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type MappingRow struct {
	LocalPortEntry  *widget.Entry
	RemoteAddrEntry *widget.Entry
	Container       *fyne.Container
}

// AppUI 管理应用程序界面
type AppUI struct {
	app    fyne.App
	window fyne.Window
	config *comm.Config

	// UI 组件
	macEntry  *widget.Entry
	autoStart *widget.Check
	startBtn  *widget.Button
	hideBtn   *widget.Button

	// 托盘
	systray  fyne.App
	quitMenu *fyne.MenuItem
	showMenu *fyne.MenuItem

	mappingsContainer *fyne.Container
	mappingRows       []*MappingRow
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

	// 动态行容器
	ui.mappingsContainer = container.NewVBox()

	// 初始化：如果没有配置，默认加一行；如果有配置，循环添加
	// 这里假设 ui.config.Mappings 是一个 map[string]string 或类似的结构
	ui.mappingRows = make([]*MappingRow, 0)
	// 示例初始化

	// 如果配置中有历史数据，加载它们
	if len(ui.config.Mappings) > 0 {
		for _, m := range ui.config.Mappings {
			ui.addMappingRow(strconv.Itoa(m.LocalPort), m.RemoteAddr)
		}
	}

	addBtn := widget.NewButtonWithIcon("添加映射行", theme.ContentAddIcon(), func() {
		ui.addMappingRow("", "")
	})

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

	scrollArea := container.NewVScroll(ui.mappingsContainer)
	scrollArea.SetMinSize(fyne.NewSize(0, 150))

	// 布局
	form := container.NewVBox(
		widget.NewLabelWithStyle("蓝牙配置", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		ui.macEntry,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("转发映射 (本地端口 -> 远程地址)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		scrollArea,
		addBtn,
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

func (ui *AppUI) createMappingRow(localPort, remoteAddr string) *MappingRow {
	row := &MappingRow{
		LocalPortEntry:  widget.NewEntry(),
		RemoteAddrEntry: widget.NewEntry(),
	}

	row.LocalPortEntry.SetText(localPort)
	row.LocalPortEntry.SetPlaceHolder("端口") // 缩短占位符

	row.RemoteAddrEntry.SetText(remoteAddr)
	row.RemoteAddrEntry.SetPlaceHolder("远程地址 (IP:Port)")

	// 删除按钮
	delBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		ui.removeMappingRow(row)
	})

	// --- 核心优化：使用自定义布局控制比例 ---
	// 这里使用一个简单的水平布局，但手动设置端口输入框的最小宽度
	portContainer := container.NewStack(row.LocalPortEntry)

	// 设置端口输入框的宽度（例如 80 像素）
	portBox := container.NewHBox(container.NewGridWrap(fyne.NewSize(80, 36), portContainer))

	// 使用 Border 布局：左侧放端口，中间放地址（自动拉伸），右侧放删除按钮
	row.Container = container.NewBorder(nil, nil, portBox, delBtn, row.RemoteAddrEntry)

	return row
}

func (ui *AppUI) removeMappingRow(row *MappingRow) {
	// 从切片中移除
	for i, r := range ui.mappingRows {
		if r == row {
			ui.mappingRows = append(ui.mappingRows[:i], ui.mappingRows[i+1:]...)
			break
		}
	}
	// 更新 UI
	ui.mappingsContainer.Remove(row.Container)
	ui.mappingsContainer.Refresh()
	//同步配置
	ui.syncConf()
	comm.StopProxy(fmt.Sprintf(":%d", row.LocalPortEntry.Text))
}
func (ui *AppUI) addMappingRow(port, addr string) {
	row := ui.createMappingRow(port, addr)
	ui.mappingRows = append(ui.mappingRows, row)
	ui.mappingsContainer.Add(row.Container)
	ui.mappingsContainer.Refresh()
	//同步配置
	ui.syncConf()
}

func (ui *AppUI) stopProxy() error {
	for _, m := range ui.config.Mappings {
		if m.LocalPort > 0 {
			// 每个端口启动一个协程，共用一个 mux
			comm.StopProxy(fmt.Sprintf(":%d", m.LocalPort))
		}
	}
	ui.startBtn.Text = "启动代理"
	ui.startBtn.Refresh()
	return nil
}

/*同步配置并且保存*/
func (ui *AppUI) syncConf() {
	ui.config.BluetoothMAC = ui.macEntry.Text
	var newMappings []comm.ProxyMapping
	for _, row := range ui.mappingRows {
		lp, _ := strconv.Atoi(row.LocalPortEntry.Text)
		if lp > 0 && row.RemoteAddrEntry.Text != "" {
			newMappings = append(newMappings, comm.ProxyMapping{
				LocalPort:  lp,
				RemoteAddr: row.RemoteAddrEntry.Text,
			})
		}
	}
	ui.config.Mappings = newMappings
	comm.SaveConfig(ui.config)
}

// startProxy 启动蓝牙代理
func (ui *AppUI) startProxy() error {
	// 验证输入
	if err := ui.macEntry.Validate(); err != nil {
		return fmt.Errorf("MAC 地址无效: %v", err)
	}
	//同步配置
	ui.syncConf()

	btRaw := comm.NewConnectBT(ui.config.BluetoothMAC)
	//多路复用
	mux := comm.NewMuxManager(btRaw)

	for _, m := range ui.config.Mappings {
		if m.LocalPort > 0 {
			// 每个端口启动一个协程，共用一个 mux
			go comm.StartPortProxy(mux, fmt.Sprintf(":%d", m.LocalPort), m.RemoteAddr)
		}
	}

	fmt.Printf("启动蓝牙代理: MAC=%s, AutoStart=%v\n",
		ui.config.BluetoothMAC, ui.config.AutoStart)
	ui.startBtn.Text = "停止代理"
	ui.startBtn.Refresh()
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
	if ui.config.AutoStart && ui.config.Mappings != nil {
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
