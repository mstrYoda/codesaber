package backend

import (
	"codesaber/backend/acp"
	"context"
	"errors"
	"time"
)

type acpModelSelector interface {
	Models() acp.ModelState
	SetModel(context.Context, string) (acp.ModelState, error)
}

func (p *sessionPrompter) Models() acp.ModelState { return p.s.Models() }
func (p *sessionPrompter) SetModel(ctx context.Context, id string) (acp.ModelState, error) {
	return p.s.SetModel(ctx, id)
}

func (a *App) ACPModels(projectID string) (acp.ModelState, error) {
	ag := a.agentFor(projectID)
	if ag == nil {
		return acp.ModelState{}, errors.New("Start a provider session to choose a model.")
	}
	selector, ok := ag.get().(acpModelSelector)
	if !ok {
		return acp.ModelState{}, errors.New("This provider does not expose model selection.")
	}
	return selector.Models(), nil
}

func (a *App) ACPSetModel(projectID, sessionID, modelID string) (acp.ModelState, error) {
	ag := a.agentFor(projectID)
	if ag == nil {
		return acp.ModelState{}, errors.New("Start a provider session to choose a model.")
	}
	session := ag.get()
	selector, ok := session.(acpModelSelector)
	if !ok {
		return acp.ModelState{}, errors.New("This provider does not expose model selection.")
	}
	if selector.Models().SessionID != sessionID {
		return acp.ModelState{}, errors.New("The session changed. Refresh the model list.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state, err := selector.SetModel(ctx, modelID)
	if a.agentFor(projectID) != ag || ag.get() != session {
		return acp.ModelState{}, errors.New("The session changed. Refresh the model list.")
	}
	return state, err
}
