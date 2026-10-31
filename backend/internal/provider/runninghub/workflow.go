package runninghub

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const (
	DefaultBaseURL = "https://www.runninghub.ai"
	WorkflowID     = "2112923637551575042"
)

var ErrTaskNotFound = errors.New("RunningHub 无法找到原任务，费用需人工核对")

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
	PromptTips   string   `json:"promptTips,omitempty"`
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
	var fallback string
	for _, result := range response.Results {
		value := strings.TrimSpace(result.URL)
		kind := strings.ToLower(strings.TrimSpace(result.OutputType))
		if kind == "" {
			parsed, err := url.Parse(value)
			if err == nil {
				kind = strings.TrimPrefix(strings.ToLower(path.Ext(parsed.Path)), ".")
			}
		}
		if value == "" || (kind != "mp4" && kind != "webm" && kind != "mov" && kind != "mkv" && !strings.HasPrefix(kind, "video/")) {
			continue
		}
		if result.NodeID == "364" {
			return value
		}
		if fallback == "" {
			fallback = value
		}
	}
	return fallback
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
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, false
	}
	return seconds, true
}

func ValidateResponse(response Response) error {
	if strings.TrimSpace(response.TaskID) == "" {
		return fmt.Errorf("RunningHub response missing taskId")
	}
	// A cancelled live task can disappear from v2 query. Absence is not a
	// confirmed cancellation and must never become evidence for a refund.
	if strings.TrimSpace(response.Status) == "" && response.ErrorCode == "1004" {
		return ErrTaskNotFound
	}
	switch NormalizeStatus(response.Status) {
	case "queued", "running", "succeeded", "failed", "cancelled":
	default:
		return fmt.Errorf("RunningHub 响应缺少可识别的任务状态")
	}
	if strings.TrimSpace(response.PromptTips) != "" {
		var validation struct {
			Result *bool `json:"result"`
		}
		if json.Unmarshal([]byte(response.PromptTips), &validation) != nil || validation.Result == nil {
			return fmt.Errorf("RunningHub 工作流校验响应无效")
		}
		if !*validation.Result {
			return fmt.Errorf("RunningHub 工作流节点校验失败，请检查参考素材和生成参数")
		}
	}
	if NormalizeStatus(response.Status) == "succeeded" && ResultURL(response) == "" {
		return fmt.Errorf("RunningHub success response missing result URL")
	}
	return nil
}
