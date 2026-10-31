package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"yingce/backend/internal/provider/runninghub"
)

// Opt-in upstream smoke test. A task ID resumes an existing task; a new paid
// submission additionally requires RUNNINGHUB_LIVE_SUBMIT=1. Never log keys or URLs.
func TestRunningHubLiveVideo(t *testing.T) {
	key := os.Getenv("RUNNINGHUB_LIVE_KEY")
	if key == "" {
		t.Skip("set RUNNINGHUB_LIVE_KEY for an authorized upstream smoke test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	config := providerConfig{APIKey: key, BaseURL: runninghub.DefaultBaseURL, Model: runninghub.WorkflowID}
	taskID := os.Getenv("RUNNINGHUB_LIVE_TASK_ID")
	if taskID == "" {
		if os.Getenv("RUNNINGHUB_LIVE_SUBMIT") != "1" {
			t.Fatal("new submission requires RUNNINGHUB_LIVE_SUBMIT=1")
		}
		duration := 1
		if value := os.Getenv("RUNNINGHUB_LIVE_SECONDS"); value != "" {
			var err error
			duration, err = strconv.Atoi(value)
			if err != nil {
				t.Fatal("invalid RUNNINGHUB_LIVE_SECONDS")
			}
		}
		inputs := runninghub.VideoInputs{Prompt: firstNonEmpty(os.Getenv("RUNNINGHUB_LIVE_PROMPT"), "A blue ceramic sphere rotates slowly on a white table, static camera."), Duration: duration, AspectRatio: "16:9", Megapixels: 0.5}
		for _, group := range []struct {
			env, mime string
			files     *[]string
		}{
			{"RUNNINGHUB_LIVE_IMAGE", "image/png", &inputs.Images}, {"RUNNINGHUB_LIVE_VIDEO", "video/mp4", &inputs.Videos}, {"RUNNINGHUB_LIVE_AUDIO", "audio/wav", &inputs.Audios},
		} {
			var files []string
			if file := os.Getenv(group.env); file != "" {
				files = append(files, file)
			}
			if value := os.Getenv(group.env + "S"); value != "" {
				if err := json.Unmarshal([]byte(value), &files); err != nil {
					t.Fatalf("%sS must be a JSON array of file paths", group.env)
				}
			}
			for _, file := range files {
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				name, err := uploadRunningHubSystemMedia(ctx, config.BaseURL, config, providerMedia{Name: filepath.Base(file), MimeType: group.mime, DataURL: dataURL(group.mime, raw)})
				if err != nil {
					t.Fatal(err)
				}
				*group.files = append(*group.files, name)
			}
		}
		nodes, err := runninghub.BuildVideoInputs(inputs)
		if err != nil {
			t.Fatal(err)
		}
		var response runninghub.Response
		if err := runningHubSystemJSON(ctx, config, config.BaseURL+runninghub.SubmitPath(config.Model), runninghub.NewSubmitRequest(nodes, firstNonEmpty(os.Getenv("RUNNINGHUB_LIVE_INSTANCE"), "default")), &response); err != nil {
			t.Fatal(err)
		}
		taskID = response.TaskID
		t.Logf("submitted taskId=%s status=%s references=%d/%d/%d", taskID, response.Status, len(inputs.Images), len(inputs.Videos), len(inputs.Audios))
		if err := runninghub.ValidateResponse(response); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("RUNNINGHUB_LIVE_CANCEL") == "1" {
		verifyRunningHubLiveCancellation(t, ctx, config, taskID)
		return
	}
	policy := defaultVideoPollPolicy()
	policy.InitialDelay = 0
	policy.Interval = 15 * time.Second
	result, err := (&Service{}).pollRunningHubSystemVideoWithPolicy(ctx, config, config.BaseURL, taskID, policy)
	if err != nil {
		t.Fatalf("taskId=%s: %v", taskID, err)
	}
	video, ok := result["video"].(map[string]interface{})
	if !ok || result["mode"] != "video" {
		t.Fatal("missing downloaded video")
	}
	_, raw, err := decodeProviderDataURL(stringValue(video["dataUrl"]))
	if err != nil || len(raw) < 1024 {
		t.Fatalf("invalid downloaded video: bytes=%d err=%v", len(raw), err)
	}
	usage, err := json.Marshal(map[string]any{"usage": result["usage"]})
	if err != nil {
		t.Fatal(err)
	}
	ms, err := runningHubRuntimeDurationMs(string(usage))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("succeeded taskId=%s downloadedBytes=%d runtimeMs=%d", taskID, len(raw), ms)
	if directory := os.Getenv("RUNNINGHUB_LIVE_OUTPUT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, taskID+".mp4"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		manifest, _ := json.MarshalIndent(map[string]any{"taskId": taskID, "bytes": len(raw), "usage": result["usage"]}, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, taskID+".json"), manifest, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func verifyRunningHubLiveCancellation(t *testing.T, ctx context.Context, config providerConfig, taskID string) {
	t.Helper()
	if err := cancelRunningHubSystemTask(ctx, config, taskID); err != nil {
		t.Fatalf("cancel taskId=%s: %v", taskID, err)
	}
	t.Logf("cancel accepted taskId=%s; awaiting confirmed terminal state", taskID)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		response, err := queryRunningHubSystemResponse(ctx, config, config.BaseURL, taskID)
		if err != nil {
			t.Fatalf("query cancellation taskId=%s: %v", taskID, err)
		}
		switch runninghub.NormalizeStatus(response.Status) {
		case "cancelled", "failed":
			t.Logf("cancel terminal taskId=%s status=%s", taskID, response.Status)
			if directory := os.Getenv("RUNNINGHUB_LIVE_OUTPUT_DIR"); directory != "" {
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				manifest, err := json.MarshalIndent(map[string]any{"taskId": taskID, "cancelAccepted": true, "status": response.Status, "usage": response.Usage}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, taskID+"-cancel.json"), manifest, 0600); err != nil {
					t.Fatal(err)
				}
			}
			return
		case "succeeded":
			t.Fatalf("taskId=%s completed before cancellation was confirmed", taskID)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("taskId=%s cancellation remains unconfirmed: %v", taskID, ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}
