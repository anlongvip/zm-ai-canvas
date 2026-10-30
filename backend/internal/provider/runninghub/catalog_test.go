package runninghub

import (
	"testing"

	"yingce/backend/internal/provider"
)

func TestCatalogMatchesRunningHubAndPublishesVideoWorkflow(t *testing.T) {
	catalog := NewCatalog()
	if !catalog.Match(DefaultBaseURL, nil) || !catalog.Match("https://www.runninghub.cn", nil) || catalog.Match("https://example.com", nil) {
		t.Fatal("unexpected RunningHub host matching")
	}
	models := catalog.AdditionalModels(provider.DiscoveryConfig{})
	if len(models) != 1 || models[0].ID != WorkflowID || models[0].Capability[0] != "video" {
		t.Fatalf("unexpected models: %#v", models)
	}
}
