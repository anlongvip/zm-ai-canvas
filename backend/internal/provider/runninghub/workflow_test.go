package runninghub

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestResponseRejectsMissingTaskAfterCancellation(t *testing.T) {
	err := ValidateResponse(Response{TaskID: "cancelled-live-task", ErrorCode: "1004"})
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task must require review, got %v", err)
	}
	if NormalizeStatus("") == "cancelled" {
		t.Fatal("missing task must not imply confirmed cancellation")
	}
}

func TestResponseRejectsFalsePromptValidationEvenWhenTaskIsRunning(t *testing.T) {
	for _, tips := range []string{`{"result":false,"node_errors":{"252":{}}}`, `not-json`, `{}`} {
		if err := ValidateResponse(Response{TaskID: "task", Status: "RUNNING", PromptTips: tips}); err == nil {
			t.Fatalf("invalid prompt validation accepted: %s", tips)
		}
	}
	if err := ValidateResponse(Response{TaskID: "task", Status: "RUNNING", PromptTips: `{"result":true}`}); err != nil {
		t.Fatal(err)
	}
}

func TestResultURLSelectsVideoOutput(t *testing.T) {
	response := Response{Results: []Result{{URL: "https://example.com/preview.png", OutputType: "png"}, {URL: "https://example.com/other.mp4", OutputType: "mp4"}, {URL: "https://example.com/final.mp4?signature=example", OutputType: "mp4", NodeID: "364"}}}
	if got := ResultURL(response); got != "https://example.com/final.mp4?signature=example" {
		t.Fatalf("video output = %s", got)
	}
	response.Results = response.Results[:1]
	if ResultURL(response) != "" {
		t.Fatal("image-only result must not be accepted as video")
	}
}

func TestRuntimeSecondsRejectsNonFiniteUsage(t *testing.T) {
	for _, value := range []any{math.NaN(), math.Inf(1), "NaN", "Inf", "-1"} {
		if _, ok := RuntimeSeconds(value); ok {
			t.Fatalf("invalid runtime accepted: %v", value)
		}
	}
}

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
	if len(inputs) != 19 || inputs[0].NodeID != "263" || inputs[0].FieldName != "text" || inputs[0].FieldValue != "a cinematic shot" {
		t.Fatalf("unexpected inputs: %#v", inputs)
	}
	if inputs[1].FieldValue != 5 || inputs[2].FieldValue != "16:9 (Widescreen)" || inputs[3].FieldValue != "0.9" {
		t.Fatalf("unexpected normalized inputs: %#v", inputs)
	}
}

func TestBuildVideoInputsPreservesReferenceOrderAndClearsUnusedMedia(t *testing.T) {
	inputs, err := BuildVideoInputs(VideoInputs{Prompt: "use @image1 then @image2", Duration: 6, AspectRatio: "16:9", Megapixels: 0.9,
		Images: []string{"openapi/first.png", "openapi/second.png"}, Videos: []string{"openapi/clip.mp4"}, Audios: []string{"openapi/sound.wav"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"51.image": "openapi/first.png", "49.image": "openapi/second.png", "27.video": "openapi/clip.mp4", "48.audio": "openapi/sound.wav"}
	for _, input := range inputs[4:] {
		key := input.NodeID + "." + input.FieldName
		expected, used := want[key]
		if !used {
			expected = "None"
		}
		if input.FieldValue != expected {
			t.Fatalf("%s = %#v; want %q", key, input.FieldValue, expected)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("reference slots missing: %v", want)
	}
}

func TestBuildVideoInputsRejectsInvalidReferences(t *testing.T) {
	for _, test := range []struct {
		name                   string
		images, videos, audios []string
	}{
		{name: "too many images", images: make([]string, 10)},
		{name: "too many videos", videos: make([]string, 4)},
		{name: "too many audios", audios: make([]string, 4)},
		{name: "empty upload name", images: []string{" "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildVideoInputs(VideoInputs{Prompt: "shot", Duration: 6, AspectRatio: "16:9", Megapixels: 0.9, Images: test.images, Videos: test.videos, Audios: test.audios})
			if err == nil {
				t.Fatal("expected invalid references to fail")
			}
		})
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
