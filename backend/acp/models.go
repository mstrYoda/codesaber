package acp

import (
	"context"
	"errors"
)

type ModelOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

type ModelState struct {
	SessionID string        `json:"sessionId"`
	CurrentID string        `json:"currentId"`
	Options   []ModelOption `json:"options"`
}

func (s *Session) Models() ModelState {
	s.modelsMu.RLock()
	defer s.modelsMu.RUnlock()
	state := s.models
	state.SessionID = s.id
	state.Options = append([]ModelOption{}, state.Options...)
	return state
}

func modelChoices(items []any, group string) []ModelOption {
	result := []ModelOption{}
	for _, item := range items {
		m, _ := item.(map[string]any)
		if nested, ok := m["options"].([]any); ok {
			label, _ := m["group"].(string)
			if label == "" {
				label, _ = m["name"].(string)
			}
			result = append(result, modelChoices(nested, label)...)
			continue
		}
		id, _ := m["value"].(string)
		name, _ := m["name"].(string)
		description, _ := m["description"].(string)
		if id == "" {
			continue
		}
		if name == "" {
			name = id
		}
		result = append(result, ModelOption{ID: id, Name: name, Description: description, Group: group})
	}
	return result
}

func (s *Session) readModels(m map[string]any) {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if configs, ok := m["configOptions"].([]any); ok {
		for _, item := range configs {
			option, _ := item.(map[string]any)
			id, _ := option["id"].(string)
			if option["category"] != "model" && id != "model" && id != "models" {
				continue
			}
			if option["type"] != "select" {
				continue
			}
			current, _ := option["currentValue"].(string)
			choices, _ := option["options"].([]any)
			s.modelConfigID = id
			s.models = ModelState{SessionID: s.id, CurrentID: current, Options: modelChoices(choices, "")}
			return
		}
		// Config updates are complete snapshots. Drop a removed model selector.
		s.modelConfigID = ""
		s.models = ModelState{SessionID: s.id, Options: []ModelOption{}}
	}
	if models, ok := m["models"].(map[string]any); ok {
		current, _ := models["currentModelId"].(string)
		choices, _ := models["availableModels"].([]any)
		options := []ModelOption{}
		for _, choice := range choices {
			model, _ := choice.(map[string]any)
			id, _ := model["modelId"].(string)
			name, _ := model["name"].(string)
			description, _ := model["description"].(string)
			if id != "" {
				if name == "" {
					name = id
				}
				options = append(options, ModelOption{ID: id, Name: name, Description: description})
			}
		}
		s.modelConfigID = ""
		s.models = ModelState{SessionID: s.id, CurrentID: current, Options: options}
	}
}

func (s *Session) observeModels(params any) {
	m, _ := params.(map[string]any)
	if id, _ := m["sessionId"].(string); id != s.id {
		return
	}
	update, _ := m["update"].(map[string]any)
	if update["sessionUpdate"] == "config_option_update" {
		s.readModels(update)
	}
	if update["sessionUpdate"] == "current_model_update" {
		id, _ := update["currentModelId"].(string)
		if id != "" {
			s.modelsMu.Lock()
			s.models.CurrentID = id
			s.modelsMu.Unlock()
		}
	}
}

func (s *Session) SetModel(ctx context.Context, id string) (ModelState, error) {
	s.promptMu.Lock()
	if s.promptOpen {
		s.promptMu.Unlock()
		return s.Models(), errors.New("Wait for the current response before changing models.")
	}
	s.promptOpen = true
	s.promptMu.Unlock()
	defer func() { s.promptMu.Lock(); s.promptOpen = false; s.promptMu.Unlock() }()
	state := s.Models()
	found := false
	for _, model := range state.Options {
		if model.ID == id {
			found = true
			break
		}
	}
	if !found {
		return state, errors.New("This model is not offered by the current provider session.")
	}
	if id == state.CurrentID {
		return state, nil
	}
	s.modelsMu.RLock()
	configID := s.modelConfigID
	s.modelsMu.RUnlock()
	method := "session/set_model"
	params := map[string]any{"sessionId": s.id, "modelId": id}
	if configID != "" {
		method = "session/set_config_option"
		params = map[string]any{"sessionId": s.id, "configId": configID, "value": id}
	}
	result, err := s.conn.Request(ctx, method, params)
	if err != nil {
		return s.Models(), errors.New("The provider could not confirm the model change. Refresh the model list and try again.")
	}
	if configID != "" {
		m, _ := result.(map[string]any)
		if _, ok := m["configOptions"].([]any); !ok {
			return s.Models(), errors.New("The provider did not confirm its model settings. Refresh and try again.")
		}
		s.readModels(m)
	} else {
		s.modelsMu.Lock()
		s.models.CurrentID = id
		s.modelsMu.Unlock()
	}
	return s.Models(), nil
}
