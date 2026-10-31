package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/provider/runninghub"
)

var _ runningHubRuntimeTaskSettlement = (*Service)(nil)
var _ runningHubTaskFailureReview = (*Service)(nil)

// Runtime and upstream spend are retained in the result or the original poll
// log. Only runtime determines the customer's charge; coins are not credits.
func (s *Service) settleRunningHubRuntimeBilling(task model.Task, providerRequestID string) (bool, error) {
	return s.settleRunningHubRuntimeBillingWithReview(task, providerRequestID, false)
}

func (s *Service) settleRunningHubRuntimeBillingWithReview(task model.Task, providerRequestID string, resolveReview bool) (bool, error) {
	if task.BillingOrderID == "" {
		return false, nil
	}
	billing := s.taskBilling()
	order, err := billing.BillingOrder(task.BillingOrderID)
	if err != nil {
		return true, err
	}
	if order == nil {
		return true, errors.New("任务计费订单不存在")
	}
	if order.Capability != "video" {
		return false, nil
	}
	matched, err := s.isRunningHubBillingTask(task)
	if err != nil || !matched {
		return matched, err
	}
	if order.UserID != task.UserID || order.TaskID != task.ID {
		return true, errors.New("任务与计费订单归属不一致")
	}
	if order.BillingMode != "per_second" {
		return true, errors.New("RunningHub 视频必须按实际运行时间计费，请核对订单计费模式")
	}
	if order.CostPricing.Configured && order.CostBillingMode != "per_second" {
		return true, errors.New("RunningHub 成本必须按实际运行时间核算，请核对订单成本模式")
	}
	if order.Status == model.BillingStatusSettled {
		return true, nil
	}
	releasedReview := order.Status == model.BillingStatusUncertain && order.RefundedAt != nil
	if releasedReview && !resolveReview {
		return true, fmt.Errorf("RunningHub 已退款订单补扣待人工核对：%w", errTaskBillingReview)
	}
	if providerRequestID == "" {
		providerRequestID = task.ProviderRequestID
	}
	if task.ProviderRequestID != "" && providerRequestID != task.ProviderRequestID {
		return true, errors.New("RunningHub 结算的上游任务 ID 不一致")
	}
	task.ProviderRequestID = providerRequestID
	durationMs, err := runningHubRuntimeDurationMs(task.ResultJSON)
	if err != nil {
		// A recovered media result can lose usage while the authenticated poll
		// still holds the original evidence. Never substitute output duration.
		observation, observationErr := s.runningHubBillingObservation(task)
		if observationErr != nil || runninghub.NormalizeStatus(observation.Status) != "succeeded" {
			return true, errors.Join(err, observationErr)
		}
		durationMs, err = runningHubRuntimeDurationMs(observation.Body)
		if err != nil {
			return true, err
		}
	}
	if order.Status == model.BillingStatusRefunded || releasedReview {
		return true, billing.RestoreRefundedBillingWithRuntime(task.BillingOrderID, providerRequestID, durationMs)
	}
	return true, billing.SettleBillingWithAudioDuration(task.BillingOrderID, providerRequestID, durationMs)
}

func (s *Service) isRunningHubBillingTask(task model.Task) (bool, error) {
	raw, err := s.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		return false, err
	}
	var input struct {
		Config struct {
			InterfaceType string `json:"interfaceType"`
		} `json:"config"`
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return false, fmt.Errorf("读取任务计费协议失败：%w", err)
	}
	if protocol := strings.TrimSpace(input.Config.InterfaceType); protocol != "" {
		return protocol == string(model.ChannelInterfaceRunningHubVideo), nil
	}
	// Successful task persistence removes config and secrets before terminal
	// billing. The authenticated query and the order's model identity remain.
	if s.repo == nil || task.BillingOrderID == "" {
		return false, nil
	}
	order, err := s.taskBilling().BillingOrder(task.BillingOrderID)
	if err != nil {
		return false, err
	}
	if order == nil || order.UserID != task.UserID || order.TaskID != task.ID {
		return false, errors.New("任务与计费订单归属不一致")
	}
	if _, err := s.runningHubBillingObservation(task); err == nil {
		return true, nil
	}
	if order.ChannelModelID == "" {
		return false, nil
	}
	item, err := s.repo.ChannelModel(order.ChannelModelID)
	if err != nil {
		return false, err
	}
	if item.ChannelID != order.ChannelID {
		return false, errors.New("计费模型与渠道归属不一致")
	}
	return item.Protocol == model.ChannelInterfaceRunningHubVideo, nil
}

func runningHubRuntimeDurationMs(raw string) (int64, error) {
	var result struct {
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return 0, errors.New("RunningHub 结果缺少有效运行时间数据")
	}
	seconds, ok := numericScalar(result.Usage["taskCostTime"])
	// Leave room for the repository's millisecond -> second ceiling. Check
	// before converting float64 to int64, including string NaN/Inf values.
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds >= float64(math.MaxInt64-999)/1000 {
		return 0, errors.New("RunningHub 结果缺少有效 taskCostTime")
	}
	return int64(math.Ceil(seconds * 1000)), nil
}

type runningHubBillingObservation struct {
	Status string
	Body   string
}

// Read only the current order's last v2 query. Submission acceptance is not
// evidence of billable success, and old attempts cannot settle a new order.
func (s *Service) runningHubBillingObservation(task model.Task) (runningHubBillingObservation, error) {
	if s.repo == nil || task.UserID == "" || task.ID == "" || task.BillingOrderID == "" || task.ProviderRequestID == "" {
		return runningHubBillingObservation{}, errors.New("RunningHub 缺少可核对的上游任务记录")
	}
	logs, err := s.repo.DiagnosticAPICallLogs(task.UserID, time.Time{}, time.Time{}, task.ID, "")
	if err != nil {
		return runningHubBillingObservation{}, err
	}
	// DiagnosticAPICallLogs returns the oldest 1000 records. A full page may
	// omit a newer state, so it cannot prove which observation is current.
	if len(logs) >= 1000 {
		return runningHubBillingObservation{}, errors.New("RunningHub 任务状态记录超出核对范围，费用待核对")
	}
	order, err := s.taskBilling().BillingOrder(task.BillingOrderID)
	if err != nil {
		return runningHubBillingObservation{}, err
	}
	if order == nil || order.UserID != task.UserID || order.TaskID != task.ID {
		return runningHubBillingObservation{}, errors.New("RunningHub 状态记录与计费订单归属不一致")
	}
	for index := len(logs) - 1; index >= 0; index-- {
		log := logs[index]
		if log.BillingOrderID != task.BillingOrderID || log.ChannelID != order.ChannelID || log.Capability != "video" || log.Path != runninghub.QueryPath ||
			(log.RequestKind != "poll" && log.RequestKind != "cancel-query") {
			continue
		}
		if log.StatusCode < 200 || log.StatusCode >= 300 {
			return runningHubBillingObservation{}, errors.New("RunningHub 最新状态查询失败，费用待核对")
		}
		detail, err := s.repo.APICallLog(log.ID)
		if err != nil {
			return runningHubBillingObservation{}, err
		}
		var response struct {
			TaskID string `json:"taskId"`
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(detail.ResponseBody), &response) != nil || response.TaskID != task.ProviderRequestID {
			return runningHubBillingObservation{}, errors.New("RunningHub 状态记录与上游任务不一致，费用待核对")
		}
		return runningHubBillingObservation{Status: response.Status, Body: detail.ResponseBody}, nil
	}
	return runningHubBillingObservation{}, errors.New("RunningHub 未找到确认的上游状态，费用待核对")
}

func (s *Service) runningHubFailureRequiresReview(task model.Task) (bool, bool) {
	if task.BillingOrderID == "" {
		return false, false
	}
	matched, err := s.isRunningHubBillingTask(task)
	if err != nil {
		return true, true
	}
	if !matched {
		return false, false
	}
	if task.ProviderRequestID == "" && s.repo != nil {
		latest, err := s.repo.Task(task.ID)
		if err != nil || latest.UserID != task.UserID || latest.BillingOrderID != task.BillingOrderID {
			return true, true
		}
		task.ProviderRequestID = latest.ProviderRequestID
	}
	observation, err := s.runningHubBillingObservation(task)
	if err != nil {
		return true, true
	}
	status := runninghub.NormalizeStatus(observation.Status)
	return true, status != "failed" && status != "cancelled"
}
