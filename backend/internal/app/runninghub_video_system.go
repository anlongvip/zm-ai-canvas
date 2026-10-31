package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"yingce/backend/internal/provider/runninghub"
)

// runRunningHubSystemVideo is the system-channel path for the current public
// RunningHub v2 API. Canvas workflow entries keep using the legacy workflow
// path until their field mapping is migrated, so the two contracts do not
// silently change together.
func (s *Service) runRunningHubSystemVideo(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	if input.Mode != "video" {
		return nil, errors.New("RunningHub 系统渠道当前只支持视频生成")
	}
	workflowID := strings.TrimSpace(input.Config.Model)
	if workflowID == "" {
		return nil, errors.New("RunningHub 视频模型缺少 workflowId")
	}
	duration, err := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
	if err != nil || duration == 0 {
		duration = 6
	}
	megapixels := runningHubResolutionMegapixels(input.Config.VQuality)
	inputs, err := runninghub.BuildVideoInputs(runninghub.VideoInputs{
		Prompt:      input.Prompt,
		Duration:    duration,
		AspectRatio: firstNonEmpty(input.Config.Size, "16:9"),
		Megapixels:  megapixels,
	})
	if err != nil {
		return nil, err
	}
	root := runningHubRootURL(input.Config.BaseURL)
	requestBody := runninghub.NewSubmitRequest(inputs, runningHubInstanceType(input.Config.VQuality))
	var created runninghub.Response
	if err := runningHubSystemJSON(ctx, input.Config, root+runninghub.SubmitPath(workflowID), requestBody, &created); err != nil {
		return nil, fmt.Errorf("RunningHub 视频提交失败：%w", err)
	}
	if err := runninghub.ValidateResponse(created); err != nil && created.TaskID == "" {
		return nil, err
	}
	if created.TaskID == "" {
		return nil, errors.New("RunningHub 视频提交没有返回 taskId")
	}
	if err := s.recordWorkflowProviderRequest(ctx, created.TaskID, "submitted", nil); err != nil {
		return nil, err
	}
	return s.pollRunningHubSystemVideo(ctx, input.Config, root, created.TaskID)
}

func (s *Service) pollRunningHubSystemVideo(ctx context.Context, config providerConfig, root, taskID string) (map[string]interface{}, error) {
	policy := defaultVideoPollPolicy()
	returnValue := map[string]interface{}{}
	_, err := runVideoPollLoop(ctx, taskID, policy, func(ctx context.Context) (videoPollOutcome, error) {
		query, err := runninghub.NewQueryRequest(taskID)
		if err != nil {
			return videoPollOutcome{}, err
		}
		var response runninghub.Response
		if err := runningHubSystemJSON(withProviderRequestKind(ctx, "poll"), config, root+runninghub.QueryPath, query, &response); err != nil {
			return videoPollOutcome{}, fmt.Errorf("RunningHub 查询任务失败：%w", err)
		}
		status := runninghub.NormalizeStatus(response.Status)
		switch status {
		case "succeeded":
			url := runninghub.ResultURL(response)
			if url == "" {
				return videoPollOutcome{}, errors.New("RunningHub 任务成功但没有返回视频地址")
			}
			url = resolveRunningHubOutputURL(root, url)
			resultValue, err := s.downloadWorkflowVideoOutputs(ctx, []string{url}, taskID, policy)
			if err != nil {
				return videoPollOutcome{}, err
			}
			if response.Usage.TaskCostTime != nil {
				resultValue["usage"] = response.Usage
			}
			returnValue = resultValue
			_ = s.updateWorkflowProviderState(ctx, taskID, "succeeded", nil)
			return videoPollOutcome{Done: true, Result: resultValue}, nil
		case "failed":
			return videoPollOutcome{}, fmt.Errorf("RunningHub 视频任务失败：%s", firstNonEmpty(response.ErrorMessage, response.ErrorCode))
		case "cancelled":
			return videoPollOutcome{}, errors.New("RunningHub 视频任务已取消")
		default:
			next := time.Now().Add(policy.Interval)
			_ = s.updateWorkflowProviderState(ctx, taskID, status, &next)
			return videoPollOutcome{}, nil
		}
	})
	if err != nil {
		return nil, err
	}
	return returnValue, nil
}

func runningHubSystemJSON(ctx context.Context, config providerConfig, endpoint string, body any, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runningHubAPIKey(config))
	ApplyOutboundHeaders(req, config.Headers)
	data, _, err := doBinary(req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("RunningHub 响应解析失败：%w", err)
	}
	return nil
}

func runningHubResolutionMegapixels(value string) float64 {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "480p", "0.5":
		return 0.5
	case "1080p", "1.5":
		return 1.5
	case "2k", "2.0":
		return 2.0
	default:
		return 0.9
	}
}

func runningHubInstanceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "plus":
		return "plus"
	case "ultra":
		return "ultra"
	default:
		return "default"
	}
}
