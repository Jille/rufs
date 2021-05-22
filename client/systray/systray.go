// +build windows darwin systray

package systray

import (
	"fmt"

	"github.com/getlantern/systray"
	"github.com/sgielen/rufs/client/icon"
)

func Run() {
	systray.Run(onSystrayReady, func() {})
}

func onSystrayReady() {
	systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetTitle("RUFS")
	systray.SetTooltip("RUFS")

	mOpen := systray.AddMenuItem("Open", "")
	mSettings := systray.AddMenuItem("Settings", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")
	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				browser.OpenURL("file://" + *mountpoint)
			case <-mSettings.ClickedCh:
				address := fmt.Sprintf("http://127.0.0.1:%d/", *httpPort)
				browser.OpenURL(address)
			case <-mQuit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}
