package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"yingce/backend/internal/provider/runninghub"
)

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
