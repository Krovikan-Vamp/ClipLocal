//go:build windows

package tray

import "github.com/getlantern/systray"

func Start() {
	go systray.Run(func() {
		systray.SetTitle("ClipLocal")
		systray.SetTooltip("ClipLocal is running")
		item := systray.AddMenuItem("Quit", "Quit ClipLocal")
		go func() {
			<-item.ClickedCh
			systray.Quit()
		}()
	}, func() {})
}
