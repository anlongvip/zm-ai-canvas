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
	if profile.Duration.Max != 20 || profile.References.MaxImages != 9 || profile.References.MaxVideos != 3 || profile.References.MaxAudios != 3 {
		t.Fatalf("unexpected references or duration: %#v", profile)
	}
	if profile.DefaultResolution != "0.9" || profile.DefaultRatio != "16:9" {
		t.Fatalf("unexpected defaults: %#v", profile)
	}
	if !runningHubContains(profile.Operations, "reference_to_video") || !runningHubContains(profile.Operations, "audio_to_video") {
		t.Fatalf("reference operations missing: %#v", profile.Operations)
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
