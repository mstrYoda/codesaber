package acp

import (
	"context"
	"os"
	"testing"
	"time"
)

func modelConfig(current string) map[string]any {
	return map[string]any{"configOptions": []any{map[string]any{
		"id": "choice", "category": "model", "type": "select", "currentValue": current,
		"options": []any{map[string]any{"group": "Provider", "options": []any{
			map[string]any{"value": "fast", "name": "Fast"}, map[string]any{"value": "deep", "name": "Deep"},
		}}},
	}}}
}

func TestModelSelectionUsesAdvertisedConfigAndConfirmation(t *testing.T) {
	h := newHarness(t, ClientHandlers{})
	h.serveAgentScript(t, func(id any, method string, params map[string]any) string {
		switch method {
		case MethodInitialize:
			return fmtResponse(id, map[string]any{"protocolVersion": 1})
		case MethodSessionNew:
			m := modelConfig("fast")
			m["sessionId"] = "models-session"
			return fmtResponse(id, m)
		case "session/set_config_option":
			if params["sessionId"] != "models-session" || params["configId"] != "choice" || params["value"] != "deep" {
				t.Errorf("wrong wire params: %v", params)
			}
			return fmtResponse(id, modelConfig("deep"))
		}
		t.Errorf("unexpected method %s", method)
		return fmtResponse(id, map[string]any{})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, err := startSessionOnConn(ctx, h.conn, "/test", ClientHandlers{})
	if err != nil {
		t.Fatal(err)
	}
	models := s.Models()
	if len(models.Options) != 2 || models.Options[0].Group != "Provider" || models.CurrentID != "fast" {
		t.Fatalf("bad catalog: %+v", models)
	}
	if _, err := s.SetModel(ctx, "invented"); err == nil {
		t.Fatal("unknown model accepted")
	}
	models, err = s.SetModel(ctx, "deep")
	if err != nil || models.CurrentID != "deep" {
		t.Fatalf("change: %+v %v", models, err)
	}
	// Provider updates must reach the catalog even while no prompt drains Notify.
	h.agentIn.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"models-session","update":{"sessionUpdate":"config_option_update","configOptions":[{"id":"choice","category":"model","type":"select","currentValue":"fast","options":[{"value":"fast","name":"Fast"}]}]}}}` + "\n"))
	deadline := time.Now().Add(time.Second)
	for s.Models().CurrentID != "fast" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.Models().CurrentID != "fast" {
		t.Fatal("idle provider update not applied")
	}
}

func TestModelSelectionRejectsBusyAndKeepsStateOnFailure(t *testing.T) {
	h := newHarness(t, ClientHandlers{})
	h.serveAgentScript(t, func(id any, method string, params map[string]any) string {
		return string(NewErrorResponse(id, RPCInternalError, "not available").MarshalLine())
	})
	s := &Session{conn: h.conn, id: "s"}
	s.readModels(modelConfig("fast"))
	s.promptOpen = true
	if _, err := s.SetModel(context.Background(), "deep"); err == nil {
		t.Fatal("changed while prompt running")
	}
	s.promptOpen = false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.SetModel(ctx, "deep"); err == nil {
		t.Fatal("expected rejected change")
	}
	if s.Models().CurrentID != "fast" {
		t.Fatal("failed change replaced model")
	}
}

func TestLegacyModelSelection(t *testing.T) {
	h := newHarness(t, ClientHandlers{})
	h.serveAgentScript(t, func(id any, method string, params map[string]any) string {
		if method != "session/set_model" || params["modelId"] != "b" {
			t.Errorf("wrong legacy request: %s %v", method, params)
		}
		return fmtResponse(id, map[string]any{})
	})
	s := &Session{conn: h.conn, id: "legacy"}
	s.readModels(map[string]any{"models": map[string]any{"currentModelId": "a", "availableModels": []any{map[string]any{"modelId": "a", "name": "A"}, map[string]any{"modelId": "b", "name": "B"}}}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := s.SetModel(ctx, "b")
	if err != nil || result.CurrentID != "b" {
		t.Fatalf("legacy change: %+v %v", result, err)
	}
}

func TestRealModelSelection(t *testing.T) {
	name := os.Getenv("CODESABER_TEST_MODELS")
	if name == "" {
		t.Skip("opt in with CODESABER_TEST_MODELS; no inference")
	}
	profile, err := Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := StartSession(ctx, t.TempDir(), *profile, ClientHandlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	initial := s.Models()
	t.Logf("provider=%s current=%s models=%d", name, initial.CurrentID, len(initial.Options))
	if len(initial.Options) == 0 {
		t.Fatal("provider offered no models")
	}
	for _, choice := range initial.Options {
		if choice.ID == initial.CurrentID {
			continue
		}
		changed, err := s.SetModel(ctx, choice.ID)
		if err != nil {
			t.Fatal(err)
		}
		if changed.CurrentID != choice.ID {
			t.Fatalf("provider confirmed %s instead of %s", changed.CurrentID, choice.ID)
		}
		restored, err := s.SetModel(ctx, initial.CurrentID)
		if err != nil || restored.CurrentID != initial.CurrentID {
			t.Fatalf("restore: %+v %v", restored, err)
		}
		t.Logf("confirmed switch to %s and restored %s; no prompt sent", choice.Name, initial.CurrentID)
		return
	}
	t.Log("provider only advertises its current model")
}
