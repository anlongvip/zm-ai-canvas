package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/provider/runninghub"
	"yingce/backend/internal/workbench"
)

// Only upstream HTTP and the unrelated ffmpeg tail-frame extraction are
// replaced. Admission, worker execution, billing and resource storage are real.
type runningHubEntryFixture struct {
	s          *Service
	db         *gorm.DB
	mu         sync.Mutex
	paths      []string
	submits    []runninghub.SubmitRequest
	video      []byte
	image      *model.Resource
	sale, cost int64
}

func newRunningHubEntryFixture(t *testing.T, tier string) *runningHubEntryFixture {
	t.Helper()
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("REDIS_URL", "")
	s, db, _, _ := creationTestService(t)
	// The admission-only fixture does not initialize the worker's runtime
	// dependencies. Use the production composition root for end-to-end execution.
	s = New(s.repo, s.dataDir)
	s.disablePiRuntime = true
	t.Cleanup(func() {
		s.closeCloudAgentPiRunners()
		s.closeApprovedCloudAgentMediaWaiters()
	})
	f := &runningHubEntryFixture{s: s, db: db, sale: 3333, cost: 2222,
		video: []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2\x00\x00\x00\x10mdatmockdata")}
	if tier == "ultra" {
		f.sale, f.cost = 6667, 4444
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.paths = append(f.paths, r.URL.Path)
		if r.URL.RawQuery != "" {
			t.Error("upstream credential must not be placed in URL")
		}
		if r.URL.Path == "/output.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(f.video)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer entry-test-key" {
			t.Errorf("incorrect upstream method/authorization for %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v2/media/upload/binary":
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer file.Close()
			actual, err := io.ReadAll(file)
			if err != nil || !bytes.Equal(actual, mediaTestPNG(t)) {
				t.Errorf("reference bytes were not hydrated from resource: %v", err)
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"fileName":"openapi/reference.png"}}`)
		case runninghub.SubmitPath(runninghub.WorkflowID):
			var body runninghub.SubmitRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			f.submits = append(f.submits, body)
			_, _ = io.WriteString(w, `{"taskId":"entry-upstream-task","status":"RUNNING"}`)
		case runninghub.QueryPath:
			var query runninghub.QueryRequest
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil || query.TaskID != "entry-upstream-task" {
				t.Errorf("incorrect query: %#v %v", query, err)
			}
			_, _ = io.WriteString(w, `{"taskId":"entry-upstream-task","status":"SUCCESS","results":[{"url":"/output.mp4","outputType":"mp4","nodeId":"364"}],"usage":{"taskCostTime":81.1,"consumeCoins":3,"consumeMoney":"0.135"}}`)
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	profile := DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceRunningHubVideo), runninghub.WorkflowID)
	profile.Video.ExecutionTier = tier
	profile.Video.References.MaxImages = 1
	encoded := mustEncodeModelCapabilityConfig(t, profile)
	if err := db.Model(&model.ModelChannel{}).Where("id = ?", "channel").Updates(map[string]any{"base_url": server.URL, "api_key": "entry-test-key"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.SystemSetting{Key: featureAvailabilitySettingKey, ValueJSON: `{"videoWorkbenchEnabled":true}`},
		&model.ChannelModel{ID: "rh-entry-model", ChannelID: "channel", ModelKey: runninghub.WorkflowID, Capability: "video", Protocol: model.ChannelInterfaceRunningHubVideo, Enabled: true, CapabilityConfigJSON: encoded, BillingMode: "per_second", PriceConfigured: true, UnitPriceMicrocredits: f.sale},
		&model.ChannelModelPriceTier{ID: "rh-entry-price", ChannelModelID: "rh-entry-model", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "per_second", UnitPriceMicrocredits: f.sale, PriceConfigured: true, Enabled: true,
			CostPricing: model.CreditCostPricing{Configured: true, UnitPriceMicrocredits: f.cost}},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", "user").Update("available_microcredits", 10_000_000).Error; err != nil {
		t.Fatal(err)
	}
	picture := mediaTestPNG(t)
	var err error
	f.image, err = s.UploadResourceFile("user", "reference.png", int64(len(picture)), "image", 2, 3, 0, bytes.NewReader(picture))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *runningHubEntryFixture) canvasRequest() CreateTaskRequest {
	// These client values deliberately disagree with the stored admin profile.
	return CreateTaskRequest{Type: "canvas_video", Prompt: "A blue sphere rotates", Model: runninghub.WorkflowID, Input: map[string]any{
		"mode": "video", "metadata": map[string]any{"source": "canvas", "nodeId": "video-node"}, "prompt": "A blue sphere rotates",
		"config": map[string]any{"channelId": "channel", "model": runninghub.WorkflowID, "videoSeconds": "5", "size": "16:9", "vquality": "0.9",
			"capabilityConfig": map[string]any{"version": 1, "video": map[string]any{"executionTier": "standard", "references": map[string]any{"maxImages": 999}}}},
		"referenceImages": []any{map[string]any{"resourceId": f.image.ID, "storageKey": "resource:" + f.image.ID, "role": "reference_image"}},
	}}
}

func (f *runningHubEntryFixture) assertCompleted(t *testing.T, id, tier string) *model.Task {
	t.Helper()
	if err := f.s.taskWorker().processNextTask(); err != nil {
		t.Fatal(err)
	}
	task, err := f.s.repo.Task(id)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusSucceeded || task.ProviderRequestID != "entry-upstream-task" {
		t.Fatalf("task did not complete: %#v", task)
	}
	resourceID, kind := taskOutputResource(task.ResultJSON, task.Type)
	if resourceID == "" || kind != "video" || strings.Contains(task.ResultJSON, "data:video") {
		t.Fatalf("output not replaced with persisted resource: %s", task.ResultJSON)
	}
	resource, body, err := f.s.OpenResource("user", resourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil || !bytes.Equal(raw, f.video) || resource.Status != model.ResourceStatusReady || resource.Kind != "video" {
		t.Fatalf("stored video bytes/status mismatch: %#v %v", resource, err)
	}
	if _, err := f.s.Resource("other-user", resourceID); err == nil {
		t.Fatal("another user could read generated video")
	}
	order, err := f.s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	account, err := f.s.repo.CreditAccount("user")
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.BillingStatusSettled || order.Quantity != 82 || order.CostQuantity != 82 || order.ActualAmountMicrocredits != f.sale*82 || account.AvailableMicrocredits != 10_000_000-f.sale*82 || account.ReservedMicrocredits != 0 {
		t.Fatalf("entry bypassed runtime settlement: %#v %#v", order, account)
	}
	cost, err := billingCreditCost(*order)
	if err != nil || cost == nil || *cost != f.cost*82 {
		t.Fatalf("wrong runtime cost: %v %v", cost, err)
	}
	var consumes int64
	if err := f.db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", order.ID, model.CreditLedgerConsume).Count(&consumes).Error; err != nil || consumes != 1 {
		t.Fatalf("consume entries=%d err=%v", consumes, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.submits) != 1 || f.submits[0].InstanceType != tier {
		t.Fatalf("admin tier not sent to upstream: %#v", f.submits)
	}
	values := map[string]any{}
	for _, node := range f.submits[0].NodeInfoList {
		values[node.NodeID+"."+node.FieldName] = node.FieldValue
	}
	if values["51.image"] != "openapi/reference.png" || values["252.megapixels"] != "0.9" || values["259.value"] != float64(5) {
		t.Fatalf("entry lost hydrated reference or screen options: %#v", values)
	}
	if len(f.paths) != 4 || f.paths[0] != "/openapi/v2/media/upload/binary" || f.paths[1] != runninghub.SubmitPath(runninghub.WorkflowID) || f.paths[2] != runninghub.QueryPath || f.paths[3] != "/output.mp4" {
		t.Fatalf("unexpected execution path: %v", f.paths)
	}
	return task
}

func TestRunningHubEntryIntegrationWorkbench(t *testing.T) {
	f := newRunningHubEntryFixture(t, "plus")
	task, err := f.s.CreateWorkbenchTask("user", WorkbenchTaskInput{Kind: WorkbenchKindVideo, Prompt: "A blue sphere rotates", Model: runninghub.WorkflowID, ChannelID: "channel",
		Parameters: map[string]any{"seconds": 5, "ratio": "16:9", "resolution": "0.9"}, References: []workbench.TaskReference{{ResourceID: f.image.ID, Kind: "image", Role: "reference"}}}, "entry-trace", "entry-request")
	if err != nil {
		t.Fatal(err)
	}
	stored := f.assertCompleted(t, task.ID, "plus")
	if stored.WorkbenchKind != WorkbenchKindVideo || stored.Operation != "reference_to_video" {
		t.Fatalf("workbench identity lost: %#v", stored)
	}
	tasks, err := f.s.WorkbenchTasks("user", WorkbenchKindVideo, false, 10)
	if err != nil || len(tasks) != 1 || tasks[0].ID != task.ID || tasks[0].PreviewKind != "video" {
		t.Fatalf("workbench output not readable: %#v %v", tasks, err)
	}
}

func TestRunningHubEntryIntegrationCanvas(t *testing.T) {
	f := newRunningHubEntryFixture(t, "ultra")
	canvas := &model.CanvasProject{ID: "rh-entry-canvas", UserID: "user", Title: "RunningHub canvas"}
	if err := f.db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	req := f.canvasRequest()
	req.ProjectID = canvas.ID
	task, err := f.s.CreateTask("user", req)
	if err != nil {
		t.Fatal(err)
	}
	stored := f.assertCompleted(t, task.ID, "ultra")
	if stored.ProjectID != canvas.ID {
		t.Fatal("canvas task lost project association")
	}
	if context := taskSummaryForOutput(*stored).ClientContext; context == nil || context.NodeID != "video-node" {
		t.Fatalf("canvas output lost originating node: %#v", context)
	}
}

func TestRunningHubMissingCancelledTaskRequiresReviewWithoutRefund(t *testing.T) {
	f := newRunningHubEntryFixture(t, "plus")
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != runninghub.QueryPath {
			t.Errorf("cancellation reconciliation must only query: %s", r.URL.Path)
		}
		queries++
		_, _ = io.WriteString(w, `{"taskId":"missing-task","status":"","errorCode":"1004"}`)
	}))
	defer server.Close()
	if err := f.db.Model(&model.ModelChannel{}).Where("id = ?", "channel").Update("base_url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	task, err := f.s.CreateTask("user", f.canvasRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
		"status": model.TaskStatusCancelled, "provider_request_id": "missing-task",
		"provider_cancel_status": model.ProviderCancelStatusRequested, "provider_cancel_attempts": 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	stored, err := f.s.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.s.repo.CreditAccount("user")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.reconcileProviderCancellation(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	stored, err = f.s.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	order, err := f.s.repo.BillingOrder(stored.BillingOrderID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.s.repo.CreditAccount("user")
	if err != nil {
		t.Fatal(err)
	}
	if queries != 1 || stored.Status != model.TaskStatusCancelled || stored.ProviderCancelStatus != model.ProviderCancelStatusUncertain ||
		stored.ProviderCancelNextCheckAt != nil || order.Status != model.BillingStatusUncertain || order.RefundedAt != nil ||
		after.AvailableMicrocredits != before.AvailableMicrocredits || after.ReservedMicrocredits != before.ReservedMicrocredits {
		t.Fatalf("missing task must stop polling and preserve funds for review: queries=%d task=%#v order=%#v before=%#v after=%#v", queries, stored, order, before, after)
	}
}

func TestRunningHubEntryIntegrationDrama(t *testing.T) {
	f := newRunningHubEntryFixture(t, "plus")
	settings := parseDramaSettings("{}")
	settings.Models.Video = DramaModelChoice{ChannelID: "channel", ModelKey: runninghub.WorkflowID}
	settings.AspectRatio, settings.VideoResolution, settings.VideoContinuity = "16:9", "0.9", DramaContinuityParallel
	settings.MediaMode, settings.ReviewMode, settings.Concurrency = DramaMediaAuto, DramaReviewAuto, 1
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: "rh-entry-drama", UserID: "user", Name: "RunningHub drama"}
	p := &model.DramaProduction{ProjectID: project.ID, UserID: "user", Status: model.DramaStatusRunning, Stage: model.DramaStageProduce, SettingsJSON: string(settingsJSON), AssetsReleased: true}
	cast := model.DramaCast{ID: "rh-entry-cast", ProjectID: project.ID, Kind: "character", Name: "Sphere", ImageResourceID: f.image.ID, ImageStatus: model.DramaMediaReady}
	for _, row := range []any{project, p,
		&model.DramaEpisode{ID: "rh-entry-episode", ProjectID: project.ID, Number: 1, Stage: model.DramaEpisodeVideo, MediaReleased: true},
		&model.DramaSegment{ID: "rh-entry-segment", ProjectID: project.ID, EpisodeID: "rh-entry-episode", SegmentIndex: 0, Revision: 1, PromptStatus: model.DramaMediaReady, VideoPrompt: "A blue sphere rotates", PromptSeconds: 5},
		&model.DramaShot{ID: "rh-entry-shot", ProjectID: project.ID, EpisodeID: "rh-entry-episode", SegmentIndex: 0, DurationMs: 5000, CastIDsJSON: `["rh-entry-cast"]`}, &cast,
	} {
		if err := f.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.GenerateDramaSegments("user", project.ID, "rh-entry-episode", GenerateDramaSegmentsRequest{Segments: []int{0}}); err != nil {
		t.Fatal(err)
	}
	run := &dramaRun{s: f.s, project: project, production: p, settings: settings, casts: []model.DramaCast{cast}, active: map[string]bool{}}
	if err := run.loadCasts(); err != nil {
		t.Fatal(err)
	}
	episode, err := f.s.repo.DramaEpisode(project.ID, "rh-entry-episode")
	if err != nil {
		t.Fatal(err)
	}
	segments, err := f.s.repo.DramaSegments(episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.enqueueSegmentVideos(episode, segments, true); err != nil {
		t.Fatal(err)
	}
	if err := f.s.repo.CreateDramaJobs(run.newJobs); err != nil {
		t.Fatal(err)
	}
	if err := run.dispatch(); err != nil {
		t.Fatal(err)
	}
	jobs, err := f.s.repo.DramaJobsByStatus(project.ID, model.DramaJobRunning)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("drama did not dispatch real generation task: %#v %v", jobs, err)
	}
	task := f.assertCompleted(t, jobs[0].TaskID, "plus")
	resourceID, _ := taskOutputResource(task.ResultJSON, task.Type)
	f.s.dramaTailFrameHook = func(userID, id string) (string, error) {
		if userID != "user" || id != resourceID {
			return "", fmt.Errorf("tail extraction did not receive generated video")
		}
		return f.image.ID, nil
	}
	if err := run.syncRunningJob(&jobs[0]); err != nil {
		t.Fatal(err)
	}
	segment, err := f.s.repo.DramaSegment(project.ID, "rh-entry-segment")
	if err != nil || segment.VideoStatus != model.DramaMediaReady || segment.VideoResourceID != resourceID || segment.TailFrameResourceID != f.image.ID {
		t.Fatalf("drama output not written back: %#v %v", segment, err)
	}
	if jobs[0].Status != model.DramaJobSucceeded || jobs[0].CostMicrocredits != f.sale*82 || p.SpentMicrocredits != f.sale*82 {
		t.Fatalf("drama budget used reservation instead of runtime: job=%#v production=%#v", jobs[0], p)
	}
}

func TestRunningHubEntryIntegrationRejectsMediaBeyondAdminCapability(t *testing.T) {
	f := newRunningHubEntryFixture(t, "ultra")
	req := f.canvasRequest()
	refs := req.Input["referenceImages"].([]any)
	req.Input["referenceImages"] = append(refs, refs[0])
	if _, err := f.s.CreateTask("user", req); err == nil {
		t.Fatal("client capabilityConfig overrode admin reference limit")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) != 0 {
		t.Fatalf("invalid media reached upstream: %v", f.paths)
	}
	var tasks, orders int64
	f.db.Model(&model.Task{}).Count(&tasks)
	f.db.Model(&model.BillingOrder{}).Count(&orders)
	account, err := f.s.repo.CreditAccount("user")
	if err != nil || tasks != 0 || orders != 0 || account.AvailableMicrocredits != 10_000_000 || account.ReservedMicrocredits != 0 {
		t.Fatalf("rejected media created task/charge: tasks=%d orders=%d account=%#v err=%v", tasks, orders, account, err)
	}
}
