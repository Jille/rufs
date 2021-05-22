// +build windows darwin systray

package systray

import (
	"runtime"

	"github.com/getlantern/systray"
	"github.com/sgielen/rufs/client/icon"
)

func Run(onOpen func(), onSettings func(), onQuit func()) {
	systray.Run(func() {
		onSystrayReady(onOpen, onSettings)
	}, onQuit)
}

func onSystrayReady(onOpen func(), onSettings func()) {
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
				onOpen()
			case <-mSettings.ClickedCh:
				onSettings()
			case <-mQuit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}

