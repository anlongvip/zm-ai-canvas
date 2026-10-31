package runninghub

import (
	"encoding/json"
	"testing"
)

func TestNormalizeStatus(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"SUCCESS", "succeeded"}, {"RUNNING", "running"}, {"QUEUED", "queued"},
		{"CANCELLED", "cancelled"}, {"CANCELED", "cancelled"}, {"FAILED", "failed"},
	} {
		if got := NormalizeStatus(test.input); got != test.want {
			t.Fatalf("NormalizeStatus(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestRuntimeSecondsAcceptsRunningHubStringUsage(t *testing.T) {
	seconds, ok := RuntimeSeconds("17")
	if !ok || seconds != 17 {
		t.Fatalf("RuntimeSeconds() = %v, %v", seconds, ok)
	}
}

func TestValidateResponseRequiresResultOnSuccess(t *testing.T) {
	if err := ValidateResponse(Response{TaskID: "task-1", Status: "SUCCESS"}); err == nil {
		t.Fatal("expected missing result URL error")
	}
	if err := ValidateResponse(Response{TaskID: "task-1", Status: "SUCCESS", Results: []Result{{URL: "https://example.com/video.mp4"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildVideoInputsUsesCurrentWorkflowNodes(t *testing.T) {
	inputs, err := BuildVideoInputs(VideoInputs{Prompt: "  a cinematic shot  ", Duration: 5, AspectRatio: "16:9 (Widescreen)", Megapixels: 0.9})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 4 || inputs[0].NodeID != "263" || inputs[0].FieldName != "text" || inputs[0].FieldValue != "a cinematic shot" {
		t.Fatalf("unexpected inputs: %#v", inputs)
	}
	if inputs[1].FieldValue != 5 || inputs[2].FieldValue != "16:9 (Widescreen)" || inputs[3].FieldValue != "0.9" {
		t.Fatalf("unexpected normalized inputs: %#v", inputs)
	}
}

func TestRequestShapesMatchRunningHubV2(t *testing.T) {
	inputs, err := BuildVideoInputs(VideoInputs{Prompt: "shot", Duration: 5, AspectRatio: "16:9", Megapixels: 0.9})
	if err != nil {
		t.Fatal(err)
	}
	if inputs[2].FieldValue != "16:9 (Widescreen)" {
		t.Fatalf("aspect ratio must use the workflow enum: %v", inputs[2].FieldValue)
	}
	submit, err := json.Marshal(NewSubmitRequest(inputs, "default"))
	if err != nil {
		t.Fatal(err)
	}
	if string(submit) == "" || SubmitPath(WorkflowID) != "/openapi/v2/run/workflow/2112923637551575042" {
		t.Fatalf("unexpected submit shape: %s", submit)
	}
	query, err := NewQueryRequest("task-1")
	if err != nil || string(mustJSON(query)) != `{"taskId":"task-1"}` {
		t.Fatalf("unexpected query: %s, %v", mustJSON(query), err)
	}
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
