package main

import (
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/wchiway/ocs_bank/internal/desktop"
)

//go:embed assets
var assets embed.FS

func main() {
	dataDir := flag.String("data-dir", "", "desktop configuration and data directory")
	flag.Parse()
	if err := run(*dataDir); err != nil {
		slog.Error("desktop stopped", "error", err)
		os.Exit(1)
	}
}

func run(root string) error {
	mygo.App.SetName("OCS Bank")
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return errors.New("无法确定用户目录，请使用 -data-dir 指定数据目录")
		}
		root = filepath.Join(base, "ocs-bank")
	}
	controller, err := desktop.New(root, slog.Default())
	if err != nil {
		return err
	}
	defer controller.Close()
	web, err := fs.Sub(assets, "assets")
	if err != nil {
		return err
	}
	mygo.SetFrontend(web)
	mygo.BindAs("Desktop", &desktopUI{controller: controller, root: root})
	var window *mygo.Window
	var windowErr error
	mygo.App.WhenReady(func() {
		defer func() {
			if recover() != nil {
				windowErr = errors.New("无法创建桌面窗口，请确认 Windows 已安装 WebView2，Linux 已安装 WebKitGTK 4.1")
				_, _ = mygo.Dialog.Message(mygo.MessageOptions{
					Type: mygo.MessageError, Title: "OCS Bank 无法启动", Message: windowErr.Error(),
				})
				mygo.App.Quit()
			}
		}()
		window = mygo.NewWindow(mygo.WindowOptions{
			Title: "OCS Bank", URL: "/", Width: 1120, Height: 800,
			MinWidth: 760, MinHeight: 620, BackgroundColor: "#f3f6fb", StateKey: "main",
		})
		window.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
			u, err := url.Parse(e.URL)
			if err != nil || u.Scheme != "mygo" || u.Host != "localhost" {
				e.PreventDefault()
			}
		})
	})
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		if window != nil {
			window.Show()
			window.Focus()
		}
	})
	mygo.App.OnWindowAllClosed(mygo.App.Quit)
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return windowErr
}

// desktopUI exposes only the operations intended for the embedded page.
type desktopUI struct {
	controller *desktop.Controller
	root       string
}

func (d *desktopUI) Settings() (desktop.Settings, error) { return d.controller.Settings() }
func (d *desktopUI) SaveSettings(r desktop.SaveRequest) (desktop.Settings, error) {
	return d.controller.SaveSettings(r)
}
func (d *desktopUI) Start() (desktop.Status, error) { return d.controller.Start() }
func (d *desktopUI) Stop() (desktop.Status, error)  { return d.controller.Stop() }
func (d *desktopUI) Status() desktop.Status         { return d.controller.Status() }
func (d *desktopUI) ExportOCS() (desktop.OCSExport, error) {
	return d.controller.ExportOCS()
}
func (d *desktopUI) CopyURL() error {
	export, err := d.controller.ExportOCS()
	if err != nil {
		return err
	}
	mygo.Clipboard.WriteText(export.URL)
	return nil
}
func (d *desktopUI) CopyOCS() error {
	export, err := d.controller.ExportOCS()
	if err != nil {
		return err
	}
	mygo.Clipboard.WriteText(export.JSON)
	return nil
}
func (d *desktopUI) SaveOCS() (bool, error) {
	export, err := d.controller.ExportOCS()
	if err != nil {
		return false, err
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title: "保存 OCS 题库配置", DefaultPath: "ocs-bank.json",
		Filters: []mygo.FileFilter{{Name: "JSON 配置", Extensions: []string{"json"}}},
	})
	if err != nil {
		return false, errors.New("无法打开保存窗口")
	}
	if path == "" {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(export.JSON+"\n"), 0600); err != nil {
		return false, errors.New("无法保存 OCS 配置，请选择可写目录")
	}
	return true, nil
}
func (d *desktopUI) OpenDataFolder() error {
	if err := os.MkdirAll(d.root, 0700); err != nil {
		return errors.New("无法创建数据目录")
	}
	if err := mygo.Shell.OpenPath(d.root); err != nil {
		return errors.New("无法打开数据目录")
	}
	return nil
}
func (d *desktopUI) Quit() { mygo.App.Quit() }
