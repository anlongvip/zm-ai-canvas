package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yingce/backend/internal/provider/runninghub"
)

func TestRunningHubAdminModelTestUsesWorkflowEndpoint(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request method or authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case runninghub.SubmitPath(runninghub.WorkflowID):
			var body runninghub.SubmitRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.NodeInfoList) == 0 {
				t.Errorf("missing workflow node inputs: %v", err)
			}
			_, _ = w.Write([]byte(`{"taskId":"task-test","status":"RUNNING"}`))
		case runninghub.QueryPath:
			_, _ = w.Write([]byte(`{"taskId":"task-test","status":"FAILED","errorMessage":"test-stop"}`))
		default:
			t.Errorf("admin test used incorrect endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	_, err := (&Service{}).runAdminChannelModelTestTask(t.Context(), canvasGenerationInput{
		Mode: "video", Prompt: "A gray circle",
		Config: providerConfig{ChannelID: "channel-test", InterfaceType: "runninghub-workflow-video", BaseURL: server.URL, APIKey: "test-key", Model: runninghub.WorkflowID, VideoSeconds: "6", Size: "16:9", VQuality: "720"},
	})
	if err == nil || !strings.Contains(err.Error(), "test-stop") {
		t.Fatalf("expected provider terminal response, got %v", err)
	}
	if len(paths) != 2 || paths[0] != runninghub.SubmitPath(runninghub.WorkflowID) || paths[1] != runninghub.QueryPath {
		t.Fatalf("unexpected request sequence: %v", paths)
	}
}

func TestRunningHubSystemVideoAdmissionDoesNotRequireClientSecrets(t *testing.T) {
	input := map[string]any{"mode": "video", "config": map[string]any{
		"interfaceType": "runninghub-workflow-video", "model": runninghub.WorkflowID, "channelId": "system-test",
	}}
	if !hasExecutableProviderVideoConfig(input) {
		t.Fatal("system video configuration must resolve credentials on the server")
	}
	delete(input["config"].(map[string]any), "channelId")
	if hasExecutableProviderVideoConfig(input) {
		t.Fatal("custom workflow without credentials must remain invalid")
	}
}

func TestRunningHubSystemJSONUsesBearerAuthorizationAndJSONBody(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["taskId"] != "task-1" {
			t.Errorf("taskId = %#v", body["taskId"])
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"taskId":"task-1","status":"CANCELLED"}`))
	}))
	defer server.Close()

	var response runninghub.Response
	err := runningHubSystemJSON(t.Context(), providerConfig{APIKey: "test-key"}, server.URL, runninghub.QueryRequest{TaskID: "task-1"}, &response)
	if err != nil {
		t.Fatal(err)
	}
	if response.TaskID != "task-1" || runninghub.NormalizeStatus(response.Status) != "cancelled" {
		t.Fatalf("response = %#v", response)
	}
}
