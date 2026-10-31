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

	"yingce/backend/internal/model"
	"yingce/backend/internal/provider/runninghub"
)

// runRunningHubSystemVideo is the system-channel path for the current public
// RunningHub v2 API. Canvas workflow entries keep using the legacy workflow
// path until their field mapping is migrated, so the two contracts do not
// silently change together.
func (s *Service) runRunningHubSystemVideo(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	return s.runRunningHubSystemVideoWithPolicy(ctx, input, defaultVideoPollPolicy())
}

func (s *Service) runRunningHubSystemVideoWithPolicy(ctx context.Context, input canvasGenerationInput, policy videoPollPolicy) (map[string]interface{}, error) {
	if input.Mode != "video" {
		return nil, errors.New("RunningHub 系统渠道当前只支持视频生成")
	}
	workflowID := strings.TrimSpace(input.Config.Model)
	if workflowID == "" {
		return nil, errors.New("RunningHub 视频模型缺少 workflowId")
	}
	root := runningHubRootURL(input.Config.BaseURL)
	if resumed := resumedProviderRequestID(ctx); resumed != "" {
		return s.pollRunningHubSystemVideoWithPolicy(ctx, input.Config, root, resumed, policy)
	}
	if input.Mask != nil {
		return nil, errors.New("RunningHub 智墨视频工作流不支持蒙版")
	}
	duration, err := strconv.Atoi(firstNonEmpty(strings.TrimSpace(input.Config.VideoSeconds), "5"))
	if err != nil {
		return nil, errors.New("RunningHub 视频时长必须为整数秒")
	}
	megapixels, err := runningHubResolutionMegapixels(input.Config.VQuality)
	if err != nil {
		return nil, err
	}
	instance := "default"
	if input.VideoCapability != nil {
		instance, err = runningHubInstanceType(input.VideoCapability.ExecutionTier)
		if err != nil {
			return nil, err
		}
	}
	videoInputs := runninghub.VideoInputs{
		Prompt:      input.Prompt,
		Duration:    duration,
		AspectRatio: firstNonEmpty(input.Config.Size, "16:9"),
		Megapixels:  megapixels,
	}
	// Validate scalar options and reference counts before any upload side effect.
	if len(input.ReferenceImages) > 9 || len(input.ReferenceVideos) > 3 || len(input.ReferenceAudios) > 3 {
		return nil, errors.New("RunningHub 智墨视频最多支持 9 张参考图、3 个参考视频和 3 个参考音频")
	}
	if _, err := runninghub.BuildVideoInputs(videoInputs); err != nil {
		return nil, err
	}
	for _, group := range []struct {
		media []providerMedia
		files *[]string
	}{
		{input.ReferenceImages, &videoInputs.Images},
		{input.ReferenceVideos, &videoInputs.Videos},
		{input.ReferenceAudios, &videoInputs.Audios},
	} {
		for _, media := range group.media {
			file, err := uploadRunningHubSystemMedia(ctx, root, input.Config, media)
			if err != nil {
				return nil, err
			}
			*group.files = append(*group.files, file)
		}
	}
	inputs, err := runninghub.BuildVideoInputs(videoInputs)
	if err != nil {
		return nil, err
	}
	requestBody := runninghub.NewSubmitRequest(inputs, instance)
	var created runninghub.Response
	if err := runningHubSystemJSON(ctx, input.Config, root+runninghub.SubmitPath(workflowID), requestBody, &created); err != nil {
		return nil, fmt.Errorf("RunningHub 视频提交失败：%w", err)
	}
	if created.TaskID == "" {
		return nil, errors.New("RunningHub 视频提交没有返回 taskId")
	}
	if err := s.recordWorkflowProviderRequest(ctx, created.TaskID, "submitted", nil); err != nil {
		return nil, err
	}
	if err := runninghub.ValidateResponse(created); err != nil {
		return nil, err
	}
	return s.pollRunningHubSystemVideoWithPolicy(ctx, input.Config, root, created.TaskID, policy)
}

func (s *Service) pollRunningHubSystemVideo(ctx context.Context, config providerConfig, root, taskID string) (map[string]interface{}, error) {
	return s.pollRunningHubSystemVideoWithPolicy(ctx, config, root, taskID, defaultVideoPollPolicy())
}

func (s *Service) pollRunningHubSystemVideoWithPolicy(ctx context.Context, config providerConfig, root, taskID string, policy videoPollPolicy) (map[string]interface{}, error) {
	returnValue := map[string]interface{}{}
	_, err := runVideoPollLoop(ctx, taskID, policy, func(ctx context.Context) (videoPollOutcome, error) {
		response, err := queryRunningHubSystemResponse(withProviderRequestKind(ctx, "poll"), config, root, taskID)
		if err != nil {
			return videoPollOutcome{}, err
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
			resultValue["usage"] = response.Usage
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

func isRunningHubSystemVideo(config providerConfig) bool {
	return config.ChannelID != "" && config.InterfaceType == string(model.ChannelInterfaceRunningHubVideo)
}

func queryRunningHubSystemResponse(ctx context.Context, config providerConfig, root, taskID string) (runninghub.Response, error) {
	query, err := runninghub.NewQueryRequest(taskID)
	if err != nil {
		return runninghub.Response{}, err
	}
	var response runninghub.Response
	if err := runningHubSystemJSON(ctx, config, root+runninghub.QueryPath, query, &response); err != nil {
		return response, fmt.Errorf("RunningHub 查询任务失败：%w", err)
	}
	if response.TaskID != taskID {
		return response, errors.New("RunningHub 查询响应的任务 ID 不一致")
	}
	return response, runninghub.ValidateResponse(response)
}

// Manual recovery queries the saved upstream ID once and never submits a job.
func (s *Service) queryRunningHubSystemVideo(ctx context.Context, config providerConfig, taskID string) (map[string]interface{}, string, error) {
	root := runningHubRootURL(config.BaseURL)
	response, err := queryRunningHubSystemResponse(withProviderRequestKind(ctx, "poll"), config, root, taskID)
	if err != nil {
		return nil, "", err
	}
	status := runninghub.NormalizeStatus(response.Status)
	switch status {
	case "succeeded":
		result, err := s.downloadWorkflowVideoOutputs(ctx, []string{resolveRunningHubOutputURL(root, runninghub.ResultURL(response))}, taskID, defaultVideoPollPolicy())
		if err != nil {
			return nil, status, err
		}
		result["usage"] = response.Usage
		return result, status, nil
	case "failed":
		return nil, status, fmt.Errorf("RunningHub 视频任务失败：%s", firstNonEmpty(response.ErrorMessage, response.ErrorCode))
	case "cancelled":
		return nil, status, errors.New("RunningHub 视频任务已取消")
	default:
		return nil, status, nil
	}
}

func cancelRunningHubSystemTask(ctx context.Context, config providerConfig, taskID string) error {
	query, err := runninghub.NewQueryRequest(taskID)
	if err != nil {
		return err
	}
	var response struct {
		Code *int `json:"code"`
	}
	// The documented cancellation API still requires apiKey in JSON as well
	// as Bearer auth. Only the server constructs this authenticated request.
	err = runningHubSystemJSON(ctx, config, runningHubRootURL(config.BaseURL)+"/task/openapi/cancel", map[string]string{"taskId": query.TaskID, "apiKey": runningHubAPIKey(config)}, &response)
	if err != nil {
		return err
	}
	if response.Code == nil || *response.Code != 0 {
		return errors.New("RunningHub 未确认收到取消请求，需查询原任务状态")
	}
	return nil
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

func runningHubResolutionMegapixels(value string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "480p", "0.5":
		return 0.5, nil
	case "1080p", "1.5":
		return 1.5, nil
	case "2k", "2.0":
		return 2.0, nil
	case "", "720", "720p", "0.9":
		return 0.9, nil
	}
	return 0, errors.New("RunningHub 视频分辨率不受当前工作流支持")
}

func runningHubInstanceType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "plus":
		return "plus", nil
	case "ultra":
		return "ultra", nil
	case "", "standard", "default":
		return "default", nil
	}
	return "", errors.New("RunningHub 执行档位仅支持 Standard、Plus、Ultra")
}
