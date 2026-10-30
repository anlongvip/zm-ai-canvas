package runninghub

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultBaseURL = "https://www.runninghub.ai"
	WorkflowID     = "2112923637551575042"
)

// NodeInput is the small, stable part of RunningHub v2 nodeInfoList. Keeping
// this separate from the full ComfyUI graph lets the provider evolve its
// workflow template without leaking provider JSON into the task model.
type NodeInput struct {
	NodeID     string `json:"nodeId"`
	FieldName  string `json:"fieldName"`
	FieldValue any    `json:"fieldValue"`
}

type SubmitRequest struct {
	AddMetadata      bool        `json:"addMetadata"`
	NodeInfoList     []NodeInput `json:"nodeInfoList"`
	InstanceType     string      `json:"instanceType,omitempty"`
	UsePersonalQueue bool        `json:"usePersonalQueue,omitempty"`
}

type QueryRequest struct {
	TaskID string `json:"taskId"`
}

func NewSubmitRequest(inputs []NodeInput, instanceType string) SubmitRequest {
	instanceType = strings.TrimSpace(instanceType)
	if instanceType == "" {
		instanceType = "default"
	}
	return SubmitRequest{AddMetadata: false, NodeInfoList: inputs, InstanceType: instanceType}
}

func NewQueryRequest(taskID string) (QueryRequest, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return QueryRequest{}, fmt.Errorf("RunningHub taskId 不能为空")
	}
	return QueryRequest{TaskID: taskID}, nil
}

type Usage struct {
	TaskCostTime           any `json:"taskCostTime,omitempty"`
	ConsumeCoins           any `json:"consumeCoins,omitempty"`
	ConsumeMoney           any `json:"consumeMoney,omitempty"`
	ThirdPartyConsumeMoney any `json:"thirdPartyConsumeMoney,omitempty"`
}

type Result struct {
	URL        string `json:"url,omitempty"`
	NodeID     string `json:"nodeId,omitempty"`
	OutputType string `json:"outputType,omitempty"`
}

type Response struct {
	TaskID       string   `json:"taskId,omitempty"`
	Status       string   `json:"status,omitempty"`
	ErrorCode    string   `json:"errorCode,omitempty"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
	Results      []Result `json:"results,omitempty"`
	Usage        Usage    `json:"usage,omitempty"`
}

func SubmitPath(workflowID string) string {
	return "/openapi/v2/run/workflow/" + strings.TrimSpace(workflowID)
}

const QueryPath = "/openapi/v2/query"

func NormalizeStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SUCCESS", "SUCCEED", "SUCCEEDED":
		return "succeeded"
	case "FAILED", "FAILURE", "ERROR":
		return "failed"
	case "CANCEL", "CANCELLED", "CANCELED":
		return "cancelled"
	case "QUEUED", "PENDING", "CREATED":
		return "queued"
	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		return "running"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func ResultURL(response Response) string {
	for _, result := range response.Results {
		if strings.TrimSpace(result.URL) != "" {
			return strings.TrimSpace(result.URL)
		}
	}
	return ""
}

func RuntimeSeconds(value any) (float64, bool) {
	var seconds float64
	switch typed := value.(type) {
	case float64:
		seconds = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		seconds = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		seconds = parsed
	default:
		return 0, false
	}
	if seconds < 0 {
		return 0, false
	}
	return seconds, true
}

func ValidateResponse(response Response) error {
	if strings.TrimSpace(response.TaskID) == "" {
		return fmt.Errorf("RunningHub response missing taskId")
	}
	if NormalizeStatus(response.Status) == "succeeded" && ResultURL(response) == "" {
		return fmt.Errorf("RunningHub success response missing result URL")
	}
	return nil
}
