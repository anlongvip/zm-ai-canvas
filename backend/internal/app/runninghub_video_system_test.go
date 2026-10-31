package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yingce/backend/internal/provider/runninghub"
)

func TestRunningHubSystemVideoUploadsReferencesWithSubmissionKey(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var uploaded []string
	var submitted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.RawQuery != "" {
			t.Error("server credential must be used only in Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v2/media/upload/binary":
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("multipart upload: %v", err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			raw, _ := io.ReadAll(file)
			if header.Filename == "" || header.Header.Get("Content-Type") == "" {
				t.Error("upload must declare a filename and content type")
			}
			uploaded = append(uploaded, string(raw))
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"fileName":"openapi/%s"}}`, raw)
		case runninghub.SubmitPath(runninghub.WorkflowID):
			submitted = true
			var body runninghub.SubmitRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.InstanceType != "plus" {
				t.Errorf("instance = %q", body.InstanceType)
			}
			want := map[string]any{"51.image": "openapi/first.png", "49.image": "openapi/second.png", "27.video": "openapi/clip.mp4", "48.audio": "openapi/sound.wav", "252.megapixels": "1.5"}
			for _, node := range body.NodeInfoList {
				key := node.NodeID + "." + node.FieldName
				if expected, ok := want[key]; ok {
					if node.FieldValue != expected {
						t.Errorf("%s = %v, want %v", key, node.FieldValue, expected)
					}
					delete(want, key)
				}
			}
			if len(want) > 0 {
				t.Errorf("missing mappings: %v", want)
			}
			_, _ = w.Write([]byte(`{"taskId":"references-task","status":"RUNNING","promptTips":"{\"result\":true}"}`))
		case runninghub.QueryPath:
			_, _ = w.Write([]byte(`{"taskId":"references-task","status":"CANCELED"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	media := func(name, mime string) providerMedia {
		return providerMedia{Name: name, MimeType: mime, DataURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(name))}
	}
	_, err := (&Service{}).runRunningHubSystemVideoWithPolicy(t.Context(), canvasGenerationInput{
		Mode: "video", Prompt: "reference shot", Config: providerConfig{BaseURL: server.URL, APIKey: "test-key", Model: runninghub.WorkflowID, VideoSeconds: "5", Size: "16:9", VQuality: "1.5"},
		VideoCapability: &VideoCapabilityConfig{ExecutionTier: "plus"},
		ReferenceImages: []providerMedia{media("first.png", "image/png"), media("second.png", "image/png")},
		ReferenceVideos: []providerMedia{media("clip.mp4", "video/mp4")}, ReferenceAudios: []providerMedia{media("sound.wav", "audio/wav")},
	}, fastVideoPollPolicy())
	if err == nil || !strings.Contains(err.Error(), "已取消") {
		t.Fatalf("expected confirmed cancellation, got %v", err)
	}
	if !submitted || strings.Join(uploaded, ",") != "first.png,second.png,clip.mp4,sound.wav" {
		t.Fatalf("upload/submit sequence: %v, %v", uploaded, submitted)
	}
}

func TestRunningHubSystemVideoResumeNeverUploadsOrResubmits(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != runninghub.QueryPath {
			t.Errorf("resume must only query, got %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		queries++
		var query runninghub.QueryRequest
		_ = json.NewDecoder(r.Body).Decode(&query)
		if query.TaskID != "original-task" {
			t.Errorf("query = %+v", query)
		}
		_, _ = w.Write([]byte(`{"taskId":"original-task","status":"FAILED","errorMessage":"resume-stop"}`))
	}))
	defer server.Close()
	ctx := context.WithValue(t.Context(), providerAnalyticsKey{}, providerAnalyticsContext{ProviderRequestID: "original-task"})
	_, err := (&Service{}).runRunningHubSystemVideoWithPolicy(ctx, canvasGenerationInput{Mode: "video", Config: providerConfig{BaseURL: server.URL, APIKey: "test-key", Model: runninghub.WorkflowID}, ReferenceImages: []providerMedia{{DataURL: "invalid"}}}, fastVideoPollPolicy())
	if queries != 1 || err == nil || !strings.Contains(err.Error(), "resume-stop") {
		t.Fatalf("queries=%d err=%v", queries, err)
	}
}

func TestRunningHubSystemVideoRejectsInvalidOptionsBeforeUpload(t *testing.T) {
	for _, tc := range []struct{ name, seconds, ratio, resolution, tier string }{
		{"fractional duration", "1.5", "16:9", "0.9", ""}, {"unsupported ratio", "5", "5:7", "0.9", ""},
		{"unsupported resolution", "5", "16:9", "bogus", ""}, {"unsupported compute", "5", "16:9", "0.9", "lite"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Service{}).runRunningHubSystemVideo(t.Context(), canvasGenerationInput{Mode: "video", Prompt: "test", Config: providerConfig{BaseURL: "https://example.invalid", Model: runninghub.WorkflowID, VideoSeconds: tc.seconds, Size: tc.ratio, VQuality: tc.resolution}, VideoCapability: &VideoCapabilityConfig{ExecutionTier: tc.tier}, ReferenceImages: []providerMedia{{DataURL: "invalid media must not be read"}}})
			if err == nil || strings.Contains(err.Error(), "base64") || strings.Contains(err.Error(), "data URL") {
				t.Fatalf("scalar validation must precede upload: %v", err)
			}
		})
	}
}

func TestRunningHubSystemUploadRequiresExplicitSuccessCode(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, body := range []string{`{"data":{"fileName":"openapi/file.png"}}`, `{"code":1,"data":{"fileName":"openapi/file.png"}}`, `{"code":0,"data":{}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := uploadRunningHubSystemMedia(t.Context(), server.URL, providerConfig{APIKey: "test-key"}, providerMedia{Name: "image.png", DataURL: "data:image/png;base64,aW1hZ2U="})
		server.Close()
		if err == nil {
			t.Fatalf("invalid success response accepted: %s", body)
		}
	}
}

func TestRunningHubSystemCancellationWaitsForConfirmedTerminalStatus(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, tc := range []struct {
		status string
		want   providerCancellationOutcome
	}{
		{"RUNNING", providerCancellationPending}, {"CANCELLED", providerCancellationConfirmed}, {"CANCELED", providerCancellationConfirmed}, {"FAILED", providerCancellationFailed}, {"SUCCESS", providerCancellationSucceeded},
	} {
		t.Run(tc.status, func(t *testing.T) {
			var cancelRequested bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing server bearer")
				}
				switch r.URL.Path {
				case "/task/openapi/cancel":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["taskId"] != "original-task" || body["apiKey"] != "test-key" {
						t.Error("incorrect documented cancel payload")
					}
					cancelRequested = true
					_, _ = w.Write([]byte(`{"code":0,"data":null}`))
				case runninghub.QueryPath:
					_, _ = fmt.Fprintf(w, `{"taskId":"original-task","status":%q,"results":[{"url":"https://example.com/final.mp4","outputType":"mp4","nodeId":"364"}]}`, tc.status)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			input := canvasGenerationInput{Config: providerConfig{ChannelID: "rh-channel", InterfaceType: "runninghub-workflow-video", BaseURL: server.URL, APIKey: "test-key"}}
			svc := &Service{}
			if !svc.supportsProviderCancellation(input.Config.InterfaceType) {
				t.Fatal("system cancellation must be supported")
			}
			if err := svc.cancelProviderTask(t.Context(), input, "original-task"); err != nil {
				t.Fatal(err)
			}
			outcome, _, err := svc.queryProviderCancellation(t.Context(), input, "original-task")
			if err != nil || !cancelRequested || outcome != tc.want {
				t.Fatalf("outcome=%s err=%v cancel=%v", outcome, err, cancelRequested)
			}
		})
	}
}

func TestRunningHubSystemManualQueryPreservesUsageAndSelectsVideo(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != runninghub.QueryPath {
			t.Errorf("manual recovery resubmitted: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"taskId":"original-task","status":"SUCCESS","results":[{"url":"https://example.com/preview.png","outputType":"png"},{"url":"data:video/mp4;base64,dGVzdC12aWRlbw==","nodeId":"364","outputType":"mp4"}],"usage":{"taskCostTime":"81","consumeMoney":"0.135","consumeCoins":0}}`))
	}))
	defer server.Close()
	result, status, err := (&Service{}).queryRunningHubSystemVideo(t.Context(), providerConfig{BaseURL: server.URL, APIKey: "test-key"}, "original-task")
	if err != nil || status != "succeeded" || result["mode"] != "video" || requests != 1 {
		t.Fatalf("result=%v status=%s err=%v requests=%d", result, status, err, requests)
	}
	encoded, _ := json.Marshal(result)
	ms, err := runningHubRuntimeDurationMs(string(encoded))
	if err != nil || ms != 81000 {
		t.Fatalf("runtime=%d err=%v", ms, err)
	}
}

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
