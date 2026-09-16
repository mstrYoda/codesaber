package backend

import (
	"codesaber/backend/acp"
	"context"
	"time"
)

func (a *App) ACPInstall(name string, repair bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if repair {
		return acp.Repair(ctx, name)
	}
	return acp.Install(ctx, name)
}
func (a *App) ACPCancelInstall() { acp.CancelInstall() }
func (a *App) ACPCheck(projectID, name string) (acp.Diagnostic, error) {
	root, err := a.resolveRoot(projectID)
	if err != nil {
		return acp.Diagnostic{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return acp.Check(ctx, root, name), nil
}
func (a *App) ACPUseSeparateSettings(name string, enabled bool) error {
	return acp.SetIsolated(name, enabled)
}
func (a *App) ACPLogin(name string) error {
	if name == "antigravity" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return acp.AuthenticateAntigravity(ctx)
	}
	return acp.Login(name)
}
