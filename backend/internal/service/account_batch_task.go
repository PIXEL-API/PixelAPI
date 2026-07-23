package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	AccountBatchTaskScopeAdmin = "admin"
	AccountBatchTaskScopeUser  = "user"

	AccountBatchTaskStatusPending   = "pending"
	AccountBatchTaskStatusRunning   = "running"
	AccountBatchTaskStatusSucceeded = "succeeded"
	AccountBatchTaskStatusFailed    = "failed"
	AccountBatchTaskStatusCanceled  = "canceled"

	AccountBatchTaskOperationAdminRefreshCredentials = "admin_refresh_credentials"
	AccountBatchTaskOperationUserRefreshCredentials  = "user_refresh_credentials"
	AccountBatchTaskOperationUserTestConnection      = "user_test_connection"
	AccountBatchTaskOperationUserRevalidateShare     = "user_revalidate_public_share"
	AccountBatchTaskOperationUserSetPublicShare      = "user_set_public_share"
	AccountBatchTaskOperationUserVerifyOpenAIPlus    = "user_verify_openai_plus"
	AccountBatchTaskOperationUserMarkOpenAIFree      = "user_mark_openai_free"
)

const (
	accountBatchTaskWorkerName       = "account_batch_task_worker"
	accountBatchTaskDefaultTimeout   = 30 * time.Minute
	accountBatchTaskDefaultInterval  = 5 * time.Second
	accountBatchTaskExecutorParallel = 6
	accountBatchTaskMaxItems         = 1000
)

type AccountBatchTask struct {
	ID           int64                  `json:"id"`
	Scope        string                 `json:"scope"`
	Operation    string                 `json:"operation"`
	Status       string                 `json:"status"`
	Total        int                    `json:"total"`
	Processed    int                    `json:"processed"`
	Success      int                    `json:"success"`
	Failed       int                    `json:"failed"`
	CreatedBy    int64                  `json:"created_by"`
	OwnerUserID  *int64                 `json:"owner_user_id,omitempty"`
	ErrorMessage *string                `json:"error_message,omitempty"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	FinishedAt   *time.Time             `json:"finished_at,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Items        []AccountBatchTaskItem `json:"items,omitempty"`
}

type AccountBatchTaskItem struct {
	ID           int64          `json:"id"`
	TaskID       int64          `json:"task_id"`
	AccountID    int64          `json:"account_id"`
	Status       string         `json:"status"`
	ErrorMessage *string        `json:"error_message,omitempty"`
	Result       map[string]any `json:"result,omitempty"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type CreateAccountBatchTaskInput struct {
	Scope       string
	Operation   string
	AccountIDs  []int64
	CreatedBy   int64
	OwnerUserID *int64
}

type AccountBatchTaskRepository interface {
	CreateTask(ctx context.Context, input CreateAccountBatchTaskInput) (*AccountBatchTask, error)
	GetTask(ctx context.Context, id int64) (*AccountBatchTask, error)
	ClaimNextPendingTask(ctx context.Context, staleRunningAfterSeconds int64) (*AccountBatchTask, error)
	ListPendingItems(ctx context.Context, taskID int64) ([]AccountBatchTaskItem, error)
	MarkItemRunning(ctx context.Context, itemID int64) error
	MarkItemSucceeded(ctx context.Context, itemID int64, result map[string]any) error
	MarkItemFailed(ctx context.Context, itemID int64, errorMessage string) error
	RefreshTaskProgress(ctx context.Context, taskID int64) (*AccountBatchTask, error)
	MarkTaskSucceeded(ctx context.Context, taskID int64) error
	MarkTaskFailed(ctx context.Context, taskID int64, errorMessage string) error
}

type AccountBatchTaskExecutor func(ctx context.Context, task *AccountBatchTask, item AccountBatchTaskItem) (map[string]any, error)

type AccountBatchTaskService struct {
	repo                AccountBatchTaskRepository
	timingWheel         *TimingWheelService
	clusterTaskExecutor *ClusterTaskExecutor

	executorsMu sync.RWMutex
	executors   map[string]AccountBatchTaskExecutor

	startOnce sync.Once
	running   int32
}

func NewAccountBatchTaskService(
	repo AccountBatchTaskRepository,
	timingWheel *TimingWheelService,
	clusterTaskExecutor ...*ClusterTaskExecutor,
) *AccountBatchTaskService {
	var leasedExecutor *ClusterTaskExecutor
	if len(clusterTaskExecutor) > 0 {
		leasedExecutor = clusterTaskExecutor[0]
	}
	return &AccountBatchTaskService{
		repo:                repo,
		timingWheel:         timingWheel,
		clusterTaskExecutor: leasedExecutor,
		executors:           map[string]AccountBatchTaskExecutor{},
	}
}

func (s *AccountBatchTaskService) Start() {
	if s == nil || s.repo == nil || s.timingWheel == nil {
		return
	}
	s.startOnce.Do(func() {
		s.timingWheel.ScheduleRecurring(accountBatchTaskWorkerName, accountBatchTaskDefaultInterval, s.runOnce)
	})
}

func (s *AccountBatchTaskService) RegisterExecutor(operation string, executor AccountBatchTaskExecutor) {
	if s == nil || executor == nil {
		return
	}
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return
	}
	s.executorsMu.Lock()
	defer s.executorsMu.Unlock()
	s.executors[operation] = executor
}

func (s *AccountBatchTaskService) CreateTask(ctx context.Context, input CreateAccountBatchTaskInput) (*AccountBatchTask, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("account batch task service not ready")
	}
	input.Scope = normalizeAccountBatchTaskScope(input.Scope)
	input.Operation = strings.TrimSpace(input.Operation)
	input.AccountIDs = normalizeBatchAccountIDs(input.AccountIDs)
	if input.Scope == "" {
		return nil, fmt.Errorf("invalid account batch task scope")
	}
	if input.Operation == "" {
		return nil, fmt.Errorf("account batch task operation is required")
	}
	if input.CreatedBy <= 0 {
		return nil, fmt.Errorf("account batch task creator is required")
	}
	if len(input.AccountIDs) == 0 {
		return nil, fmt.Errorf("account_ids is required")
	}
	if len(input.AccountIDs) > accountBatchTaskMaxItems {
		return nil, fmt.Errorf("too many account_ids; maximum is %d", accountBatchTaskMaxItems)
	}
	task, err := s.repo.CreateTask(ctx, input)
	if err != nil {
		return nil, err
	}
	go s.runOnce()
	return task, nil
}

func (s *AccountBatchTaskService) GetTask(ctx context.Context, id int64) (*AccountBatchTask, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("account batch task service not ready")
	}
	if id <= 0 {
		return nil, fmt.Errorf("invalid account batch task id")
	}
	return s.repo.GetTask(ctx, id)
}

func (s *AccountBatchTaskService) runOnce() {
	if s == nil || s.repo == nil {
		return
	}
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)

	ctx, cancel := context.WithTimeout(context.Background(), accountBatchTaskDefaultTimeout)
	defer cancel()

	processed := false
	runTask := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		var err error
		processed, err = s.processNextTask(taskCtx, guard)
		return err
	}
	var err error
	if s.clusterTaskExecutor == nil {
		err = runTask(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.clusterTaskExecutor.Run(ctx, accountBatchTaskWorkerName, runTask)
	}
	if err != nil {
		logger.LegacyPrintf("service.account_batch_task", "run account batch task worker failed: %v", err)
		return
	}
	if processed {
		go s.runOnce()
	}
}

func (s *AccountBatchTaskService) processNextTask(
	ctx context.Context,
	guard *ClusterLeaseGuard,
) (bool, error) {
	if err := guard.Check(ctx); err != nil {
		return false, err
	}
	task, err := s.repo.ClaimNextPendingTask(ctx, int64(accountBatchTaskDefaultTimeout.Seconds()))
	if err != nil {
		return false, fmt.Errorf("claim pending account batch task: %w", err)
	}
	if task == nil {
		return false, nil
	}
	if err := s.executeTask(ctx, task, guard); err != nil {
		return true, fmt.Errorf("execute account batch task %d: %w", task.ID, err)
	}
	return true, nil
}

func (s *AccountBatchTaskService) executeTask(
	ctx context.Context,
	task *AccountBatchTask,
	guard *ClusterLeaseGuard,
) error {
	executor := s.executorFor(task.Operation)
	if executor == nil {
		msg := "account batch task executor is not registered"
		if err := guard.Check(ctx); err != nil {
			return err
		}
		_ = s.repo.MarkTaskFailed(context.WithoutCancel(ctx), task.ID, msg)
		return fmt.Errorf("%s: %s", msg, task.Operation)
	}

	items, err := s.repo.ListPendingItems(ctx, task.ID)
	if err != nil {
		if guardErr := guard.Check(ctx); guardErr != nil {
			return guardErr
		}
		_ = s.repo.MarkTaskFailed(context.WithoutCancel(ctx), task.ID, err.Error())
		return err
	}
	if len(items) == 0 {
		return s.finishTaskByProgress(ctx, task.ID, guard)
	}

	sem := make(chan struct{}, accountBatchTaskExecutorParallel)
	var wg sync.WaitGroup
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		item := item
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.executeItem(ctx, task, item, executor, guard)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.finishTaskByProgress(ctx, task.ID, guard)
}

func (s *AccountBatchTaskService) executeItem(
	ctx context.Context,
	task *AccountBatchTask,
	item AccountBatchTaskItem,
	executor AccountBatchTaskExecutor,
	guard *ClusterLeaseGuard,
) {
	if err := guard.Check(ctx); err != nil {
		return
	}
	if err := s.repo.MarkItemRunning(ctx, item.ID); err != nil {
		slog.Warn("account batch task mark item running failed", "task_id", task.ID, "item_id", item.ID, "error", err)
		return
	}
	if err := guard.Check(ctx); err != nil {
		return
	}
	result, err := executor(ctx, task, item)
	if err != nil {
		if guardErr := guard.Check(context.WithoutCancel(ctx)); guardErr != nil {
			return
		}
		_ = s.repo.MarkItemFailed(context.WithoutCancel(ctx), item.ID, trimAccountBatchError(err.Error()))
		return
	}
	if err := guard.Check(context.WithoutCancel(ctx)); err != nil {
		return
	}
	_ = s.repo.MarkItemSucceeded(context.WithoutCancel(ctx), item.ID, result)
}

func (s *AccountBatchTaskService) finishTaskByProgress(
	ctx context.Context,
	taskID int64,
	guard *ClusterLeaseGuard,
) error {
	if err := guard.Check(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	task, err := s.repo.RefreshTaskProgress(context.WithoutCancel(ctx), taskID)
	if err != nil {
		return err
	}
	if err := guard.Check(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	if task.Failed > 0 {
		return s.repo.MarkTaskFailed(
			context.WithoutCancel(ctx),
			taskID,
			fmt.Sprintf("%d account operations failed", task.Failed),
		)
	}
	return s.repo.MarkTaskSucceeded(context.WithoutCancel(ctx), taskID)
}

func (s *AccountBatchTaskService) executorFor(operation string) AccountBatchTaskExecutor {
	s.executorsMu.RLock()
	defer s.executorsMu.RUnlock()
	return s.executors[operation]
}

func normalizeAccountBatchTaskScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case AccountBatchTaskScopeAdmin:
		return AccountBatchTaskScopeAdmin
	case AccountBatchTaskScopeUser:
		return AccountBatchTaskScopeUser
	default:
		return ""
	}
}

func normalizeBatchAccountIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func trimAccountBatchError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		return message[:500]
	}
	return message
}
