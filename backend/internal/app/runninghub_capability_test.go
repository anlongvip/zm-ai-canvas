package app

import (
	"testing"

	"yingce/backend/internal/model"
)

func TestRunningHubVideoCapabilityMatchesPublishedWorkflow(t *testing.T) {
	profile := DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceRunningHubVideo), "2112923637551575042").Video
	if profile == nil {
		t.Fatal("RunningHub video capability is nil")
	}
	if profile.Duration.Max != 15 || profile.References.MaxImages != 9 || profile.References.MaxVideos != 3 || profile.References.MaxAudios != 3 {
		t.Fatalf("unexpected references or duration: %#v", profile)
	}
	if profile.DefaultResolution != "0.9" || profile.DefaultRatio != "16:9" {
		t.Fatalf("unexpected defaults: %#v", profile)
	}
	if !runningHubContains(profile.Operations, "reference_to_video") || !runningHubContains(profile.Operations, "audio_to_video") {
		t.Fatalf("reference operations missing: %#v", profile.Operations)
	}
}

func TestRunningHubNormalizesPreviouslySavedProfile(t *testing.T) {
	protocol := string(model.ChannelInterfaceRunningHubVideo)
	for _, duration := range []VideoDurationConfig{{Selection: "range", Min: 1, Max: 20, Step: 1, Default: 20}, {Selection: "enum", Values: []int{5, 15, 20}, Default: 20}} {
		config := DefaultModelCapabilityConfigForModel(protocol, "2112923637551575042")
		config.Video.ExecutionTier, config.Video.Duration = "ultra", duration
		normalized, err := NormalizeModelCapabilityConfigForModel("video", protocol, "2112923637551575042", config)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.Video.ExecutionTier != "standard" || videoDurationAllowed(normalized.Video.Duration, 20) || !videoDurationAllowed(normalized.Video.Duration, 15) {
			t.Fatalf("unexpected effective profile: %#v", normalized.Video)
		}
	}
}

func runningHubContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
