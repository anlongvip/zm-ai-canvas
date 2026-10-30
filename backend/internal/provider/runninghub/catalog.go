package runninghub

import (
	"net/url"
	"strings"

	"yingce/backend/internal/provider"
)

type Catalog struct{}

func NewCatalog() provider.ModelCatalogPlugin { return Catalog{} }

func (Catalog) GetProviderID() string { return "runninghub" }

func (Catalog) Match(baseURL string, _ map[string]string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "www.runninghub.ai" || host == "runninghub.ai" || host == "www.runninghub.cn" || host == "runninghub.cn"
}

func (Catalog) AdditionalModels(_ provider.DiscoveryConfig) []provider.Model {
	return []provider.Model{{
		ID:                     WorkflowID,
		DisplayName:            "智墨 AI 视频",
		Provider:               "runninghub",
		Capability:             []string{"video"},
		SupportedEndpointTypes: []string{"runninghub-workflow-video"},
		APIPath:                SubmitPath(WorkflowID),
		Metadata: map[string]any{
			"workflowId": WorkflowID,
			"workflow":   "智墨AI创作平台",
			"interface":  "runninghub-workflow-video",
		},
	}}
}

func (Catalog) GetMetadata() provider.ProviderMetadata {
	return provider.ProviderMetadata{
		Name:             "RunningHub",
		Version:          "0.1.0",
		Description:      "RunningHub Workflow 视频目录",
		Author:           "智墨",
		SupportedRegions: []string{"cn", "global"},
	}
}
