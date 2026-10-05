package adapter

import "github.com/wailsapp/wails/v3/pkg/application"

// Stable window names used with app.Window.GetByName.
const (
	WelcomeWindowName   = "welcome"
	WorkspaceWindowName = "workspace"
)

// macChrome is the shared macOS window dressing: hidden native titlebar with
// retained traffic lights (MacTitleBarHiddenInset) and content extending to
// the window top so the custom HTML titlebar renders underneath.
func macOptions() application.MacWindow {
	return application.MacWindow{
		InvisibleTitleBarHeight: 34,
		Backdrop:                application.MacBackdropTranslucent,
		TitleBar:                application.MacTitleBarHiddenInset,
	}
}

// OpenWelcomeWindow shows the welcome window, creating it on first use.
// Centered 800x520, hash-routed to #welcome.
func OpenWelcomeWindow() {
	app := application.Get()
	if app == nil {
		return
	}
	if w, ok := app.Window.GetByName(WelcomeWindowName); ok {
		w.Show()
		return
	}
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             WelcomeWindowName,
		Title:            "codesaber",
		Width:            800,
		Height:           520,
		Mac:              macOptions(),
		BackgroundColour: application.NewRGB(30, 31, 34),
		URL:              "/#welcome",
	})
}

// CloseWelcomeWindow closes the welcome window after the workspace opens.
// A hidden welcome window would keep the app alive when the workspace closes.
func CloseWelcomeWindow() {
	app := application.Get()
	if app == nil {
		return
	}
	if w, ok := app.Window.GetByName(WelcomeWindowName); ok {
		w.Close()
	}
}

// ShowWelcomeWindow brings the welcome window up (shown after the last
// project is removed or at app start).
func ShowWelcomeWindow() {
	OpenWelcomeWindow()
}

// EnsureWorkspaceWindow opens the workspace window if none exists, otherwise
// shows and focuses the existing one.
func EnsureWorkspaceWindow() {
	app := application.Get()
	if app == nil {
		return
	}
	if w, ok := app.Window.GetByName(WorkspaceWindowName); ok {
		w.Show()
		return
	}
	w := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             WorkspaceWindowName,
		Title:            "codesaber",
		Width:            1000,
		Height:           618,
		Mac:              macOptions(),
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/#workspace",
	})
	// beta.20: runtime-created windows only show after WebViewDidFinishNavigation,
	// which doesn't reliably fire here — show explicitly.
	w.Show()
}
