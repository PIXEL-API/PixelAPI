package service

import (
	"context"
	"database/sql"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// BuildInfo contains build information
type BuildInfo struct {
	Version   string
	BuildType string
	Commit    string
	Date      string
}

// ProvidePricingService creates and initializes PricingService
func ProvidePricingService(cfg *config.Config, remoteClient PricingRemoteClient) (*PricingService, error) {
	svc := NewPricingService(cfg, remoteClient)
	if err := svc.Initialize(); err != nil {
		// Pricing service initialization failure should not block startup, use fallback prices
		println("[Service] Warning: Pricing service initialization failed:", err.Error())
	}
	return svc, nil
}

// ProvideUpdateService creates UpdateService with BuildInfo
func ProvideUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, buildInfo BuildInfo) *UpdateService {
	return NewUpdateService(cache, githubClient, buildInfo.Version, buildInfo.BuildType)
}

// ProvideEmailQueueService creates EmailQueueService with default worker count
func ProvideEmailQueueService(emailService *EmailService) *EmailQueueService {
	return NewEmailQueueService(emailService, 3)
}

// ProvideOAuthRefreshAPI creates OAuthRefreshAPI with the default lock TTL.
func ProvideOAuthRefreshAPI(accountRepo AccountRepository, tokenCache GeminiTokenCache) *OAuthRefreshAPI {
	return NewOAuthRefreshAPI(accountRepo, tokenCache)
}

// ProvideTokenRefreshService creates and starts TokenRefreshService
func ProvideTokenRefreshService(
	accountRepo AccountRepository,
	oauthService *OAuthService,
	openaiOAuthService *OpenAIOAuthService,
	geminiOAuthService *GeminiOAuthService,
	antigravityOAuthService *AntigravityOAuthService,
	grokOAuthService *GrokOAuthService,
	cacheInvalidator TokenCacheInvalidator,
	schedulerCache SchedulerCache,
	cfg *config.Config,
	tempUnschedCache TempUnschedCache,
	privacyClientFactory PrivacyClientFactory,
	proxyRepo ProxyRepository,
	refreshAPI *OAuthRefreshAPI,
	taskExecutor *ClusterTaskExecutor,
) *TokenRefreshService {
	svc := NewTokenRefreshService(accountRepo, oauthService, openaiOAuthService, geminiOAuthService, antigravityOAuthService, cacheInvalidator, schedulerCache, cfg, tempUnschedCache, grokOAuthService)
	// 注入 OpenAI privacy opt-out 依赖
	svc.SetPrivacyDeps(privacyClientFactory, proxyRepo)
	// 注入统一 OAuth 刷新 API（消除 TokenRefreshService 与 TokenProvider 之间的竞争条件）
	svc.SetRefreshAPI(refreshAPI)
	// 调用侧显式注入后台刷新策略，避免策略漂移
	svc.SetRefreshPolicy(DefaultBackgroundRefreshPolicy())
	svc.SetClusterTaskExecutor(taskExecutor)
	svc.Start()
	return svc
}

// ProvideClaudeTokenProvider creates ClaudeTokenProvider with OAuthRefreshAPI injection
func ProvideClaudeTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	oauthService *OAuthService,
	refreshAPI *OAuthRefreshAPI,
) *ClaudeTokenProvider {
	p := NewClaudeTokenProvider(accountRepo, tokenCache, oauthService)
	executor := NewClaudeTokenRefresher(oauthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(ClaudeProviderRefreshPolicy())
	return p
}

// ProvideOpenAITokenProvider creates OpenAITokenProvider with OAuthRefreshAPI injection
func ProvideOpenAITokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	openaiOAuthService *OpenAIOAuthService,
	refreshAPI *OAuthRefreshAPI,
) *OpenAITokenProvider {
	p := NewOpenAITokenProvider(accountRepo, tokenCache, openaiOAuthService)
	executor := NewOpenAITokenRefresher(openaiOAuthService, accountRepo)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(OpenAIProviderRefreshPolicy())
	return p
}

func ProvideOpenAIQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	tokenProvider *OpenAITokenProvider,
	privacyClientFactory PrivacyClientFactory,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
) *OpenAIQuotaService {
	svc := NewOpenAIQuotaService(accountRepo, proxyRepo, tokenProvider, privacyClientFactory)
	svc.SetAgentIdentityWSInvalidator(agentIdentityWSInvalidator)
	return svc
}

func ProvideGrokQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	tokenProvider *GrokTokenProvider,
	httpUpstream HTTPUpstream,
	usageLogRepo UsageLogRepository,
	cfg *config.Config,
	settingService *SettingService,
) *GrokQuotaService {
	svc := NewGrokQuotaService(accountRepo, proxyRepo, tokenProvider, httpUpstream, usageLogRepo)
	svc.SetURLSecurityPolicy(cfg, settingService)
	return svc
}

// ProvideGrokTokenProvider creates GrokTokenProvider with OAuthRefreshAPI injection.
func ProvideGrokTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	grokOAuthService *GrokOAuthService,
	refreshAPI *OAuthRefreshAPI,
	tempUnschedCache TempUnschedCache,
) *GrokTokenProvider {
	p := NewGrokTokenProvider(accountRepo, tokenCache)
	executor := NewGrokTokenRefresher(grokOAuthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(GrokProviderRefreshPolicy())
	p.SetTempUnschedCache(tempUnschedCache)
	return p
}

// ProvideGeminiTokenProvider creates GeminiTokenProvider with OAuthRefreshAPI injection
func ProvideGeminiTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	geminiOAuthService *GeminiOAuthService,
	refreshAPI *OAuthRefreshAPI,
) *GeminiTokenProvider {
	p := NewGeminiTokenProvider(accountRepo, tokenCache, geminiOAuthService)
	executor := NewGeminiTokenRefresher(geminiOAuthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(GeminiProviderRefreshPolicy())
	return p
}

// ProvideAntigravityTokenProvider creates AntigravityTokenProvider with OAuthRefreshAPI injection
func ProvideAntigravityTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	antigravityOAuthService *AntigravityOAuthService,
	refreshAPI *OAuthRefreshAPI,
	tempUnschedCache TempUnschedCache,
) *AntigravityTokenProvider {
	p := NewAntigravityTokenProvider(accountRepo, tokenCache, antigravityOAuthService)
	executor := NewAntigravityTokenRefresher(antigravityOAuthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(AntigravityProviderRefreshPolicy())
	p.SetTempUnschedCache(tempUnschedCache)
	return p
}

// ProvideDashboardAggregationService 创建并启动仪表盘聚合服务
func ProvideDashboardAggregationService(
	repo DashboardAggregationRepository,
	timingWheel *TimingWheelService,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *DashboardAggregationService {
	svc := NewDashboardAggregationService(repo, timingWheel, cfg)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideUsageCleanupService 创建并启动使用记录清理任务服务
func ProvideUsageCleanupService(
	repo UsageCleanupRepository,
	timingWheel *TimingWheelService,
	dashboardAgg *DashboardAggregationService,
	backup *BackupService,
	settingRepo SettingRepository,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *UsageCleanupService {
	svc := NewUsageCleanupServiceWithBackup(repo, timingWheel, dashboardAgg, backup, settingRepo, cfg)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideAccountBatchTaskService creates account batch task service.
// The worker is started after handlers register all operation executors.
func ProvideAccountBatchTaskService(
	repo AccountBatchTaskRepository,
	timingWheel *TimingWheelService,
	taskExecutor *ClusterTaskExecutor,
) *AccountBatchTaskService {
	return NewAccountBatchTaskService(repo, timingWheel, taskExecutor)
}

func ProvideAccountBatchTaskServices(svc *AccountBatchTaskService) []*AccountBatchTaskService {
	if svc == nil {
		return nil
	}
	return []*AccountBatchTaskService{svc}
}

// ProvideAccountExpiryService creates and starts AccountExpiryService.
func ProvideAccountExpiryService(
	accountRepo AccountRepository,
	taskExecutor *ClusterTaskExecutor,
) *AccountExpiryService {
	svc := NewAccountExpiryService(accountRepo, time.Minute, taskExecutor)
	svc.Start()
	return svc
}

// ProvideAccountErrorCleanupService creates and starts AccountErrorCleanupService.
func ProvideAccountErrorCleanupService(
	accountRepo AccountRepository,
	taskExecutor *ClusterTaskExecutor,
) *AccountErrorCleanupService {
	cleanupRepo, ok := accountRepo.(AccountErrorCleanupRepository)
	if !ok {
		return nil
	}
	svc := NewAccountErrorCleanupService(cleanupRepo, time.Minute, taskExecutor)
	svc.Start()
	return svc
}

// ProvideSubscriptionExpiryService creates and starts SubscriptionExpiryService.
func ProvideSubscriptionExpiryService(
	userSubRepo UserSubscriptionRepository,
	taskExecutor *ClusterTaskExecutor,
) *SubscriptionExpiryService {
	svc := NewSubscriptionExpiryService(userSubRepo, time.Minute, taskExecutor)
	svc.Start()
	return svc
}

// ProvideTimingWheelService creates and starts TimingWheelService
func ProvideTimingWheelService() (*TimingWheelService, error) {
	svc, err := NewTimingWheelService()
	if err != nil {
		return nil, err
	}
	svc.Start()
	return svc, nil
}

// ProvideDeferredService creates and starts DeferredService
func ProvideDeferredService(accountRepo AccountRepository, timingWheel *TimingWheelService) *DeferredService {
	svc := NewDeferredService(accountRepo, timingWheel, 10*time.Second)
	svc.Start()
	return svc
}

// ProvideConcurrencyService creates ConcurrencyService and starts slot cleanup worker.
func ProvideConcurrencyService(
	cache ConcurrencyCache,
	accountRepo AccountRepository,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *ConcurrencyService {
	svc := NewConcurrencyService(cache, taskExecutor)
	if cfg != nil {
		svc.StartSlotCleanupWorker(accountRepo, cfg.Gateway.Scheduling.SlotCleanupInterval)
	}
	return svc
}

// ProvideUserMessageQueueService 创建用户消息串行队列服务并启动清理 worker
func ProvideUserMessageQueueService(
	cache UserMsgQueueCache,
	rpmCache RPMCache,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *UserMessageQueueService {
	svc := NewUserMessageQueueService(cache, rpmCache, &cfg.Gateway.UserMessageQueue, taskExecutor)
	if cfg.Gateway.UserMessageQueue.CleanupIntervalSeconds > 0 {
		svc.StartCleanupWorker(time.Duration(cfg.Gateway.UserMessageQueue.CleanupIntervalSeconds) * time.Second)
	}
	return svc
}

// ProvideSchedulerSnapshotService creates and starts SchedulerSnapshotService.
func ProvideSchedulerSnapshotService(
	cache SchedulerCache,
	outboxRepo SchedulerOutboxRepository,
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	settingRepo SettingRepository,
	cfg *config.Config,
) *SchedulerSnapshotService {
	svc := NewSchedulerSnapshotService(cache, outboxRepo, accountRepo, groupRepo, cfg)
	svc.SetSettingRepository(settingRepo)
	svc.Start()
	return svc
}

// ProvideRateLimitService creates RateLimitService with optional dependencies.
func ProvideRateLimitService(
	accountRepo AccountRepository,
	usageRepo UsageLogRepository,
	cfg *config.Config,
	geminiQuotaService *GeminiQuotaService,
	tempUnschedCache TempUnschedCache,
	timeoutCounterCache TimeoutCounterCache,
	openAI403CounterCache OpenAI403CounterCache,
	settingService *SettingService,
	tokenCacheInvalidator TokenCacheInvalidator,
) *RateLimitService {
	svc := NewRateLimitService(accountRepo, usageRepo, cfg, geminiQuotaService, tempUnschedCache)
	svc.SetTimeoutCounterCache(timeoutCounterCache)
	svc.SetOpenAI403CounterCache(openAI403CounterCache)
	svc.SetSettingService(settingService)
	svc.SetTokenCacheInvalidator(tokenCacheInvalidator)
	return svc
}

// ProvideOpsMetricsCollector creates and starts OpsMetricsCollector.
func ProvideOpsMetricsCollector(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	accountRepo AccountRepository,
	concurrencyService *ConcurrencyService,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *OpsMetricsCollector {
	collector := NewOpsMetricsCollector(opsRepo, settingRepo, accountRepo, concurrencyService, db, redisClient, cfg)
	collector.taskExecutor = taskExecutor
	collector.Start()
	return collector
}

// ProvideOpsAggregationService creates and starts OpsAggregationService (hourly/daily pre-aggregation).
func ProvideOpsAggregationService(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *OpsAggregationService {
	svc := NewOpsAggregationService(opsRepo, settingRepo, db, redisClient, cfg)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideOpsAlertEvaluatorService creates and starts OpsAlertEvaluatorService.
func ProvideOpsAlertEvaluatorService(
	opsService *OpsService,
	opsRepo OpsRepository,
	emailService *EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *OpsAlertEvaluatorService {
	svc := NewOpsAlertEvaluatorService(opsService, opsRepo, emailService, redisClient, cfg)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideOpsCleanupService creates and starts OpsCleanupService (cron scheduled).
// channelMonitorSvc 让维护任务（聚合 + 历史/聚合软删）跟随 ops 清理 cron 一起跑，
// 共享 leader lock + heartbeat。
func ProvideOpsCleanupService(
	opsService *OpsService,
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	backupSvc *BackupService,
	taskExecutor *ClusterTaskExecutor,
) *OpsCleanupService {
	svc := NewOpsCleanupService(opsRepo, settingRepo, db, redisClient, cfg, channelMonitorSvc, backupSvc)
	svc.taskExecutor = taskExecutor
	opsService.setCleanupSettingsApplier(svc)
	svc.Start()
	return svc
}

func ProvideOpsSystemLogSink(opsRepo OpsRepository) *OpsSystemLogSink {
	sink := NewOpsSystemLogSink(opsRepo)
	sink.Start()
	logger.SetSink(sink)
	return sink
}

func buildIdempotencyConfig(cfg *config.Config) IdempotencyConfig {
	idempotencyCfg := DefaultIdempotencyConfig()
	if cfg != nil {
		if cfg.Idempotency.DefaultTTLSeconds > 0 {
			idempotencyCfg.DefaultTTL = time.Duration(cfg.Idempotency.DefaultTTLSeconds) * time.Second
		}
		if cfg.Idempotency.SystemOperationTTLSeconds > 0 {
			idempotencyCfg.SystemOperationTTL = time.Duration(cfg.Idempotency.SystemOperationTTLSeconds) * time.Second
		}
		if cfg.Idempotency.ProcessingTimeoutSeconds > 0 {
			idempotencyCfg.ProcessingTimeout = time.Duration(cfg.Idempotency.ProcessingTimeoutSeconds) * time.Second
		}
		if cfg.Idempotency.FailedRetryBackoffSeconds > 0 {
			idempotencyCfg.FailedRetryBackoff = time.Duration(cfg.Idempotency.FailedRetryBackoffSeconds) * time.Second
		}
		if cfg.Idempotency.MaxStoredResponseLen > 0 {
			idempotencyCfg.MaxStoredResponseLen = cfg.Idempotency.MaxStoredResponseLen
		}
		idempotencyCfg.ObserveOnly = cfg.Idempotency.ObserveOnly
	}
	return idempotencyCfg
}

func ProvideIdempotencyCoordinator(repo IdempotencyRepository, cfg *config.Config) *IdempotencyCoordinator {
	coordinator := NewIdempotencyCoordinator(repo, buildIdempotencyConfig(cfg))
	SetDefaultIdempotencyCoordinator(coordinator)
	return coordinator
}

func ProvideSystemOperationLockService(repo IdempotencyRepository, cfg *config.Config) *SystemOperationLockService {
	return NewSystemOperationLockService(repo, buildIdempotencyConfig(cfg))
}

func ProvideIdempotencyCleanupService(
	repo IdempotencyRepository,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *IdempotencyCleanupService {
	svc := NewIdempotencyCleanupService(repo, cfg, taskExecutor)
	svc.Start()
	return svc
}

// ProvideScheduledTestService creates ScheduledTestService.
func ProvideScheduledTestService(
	planRepo ScheduledTestPlanRepository,
	resultRepo ScheduledTestResultRepository,
) *ScheduledTestService {
	return NewScheduledTestService(planRepo, resultRepo)
}

// ProvideScheduledTestRunnerService creates and starts ScheduledTestRunnerService.
func ProvideScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *ScheduledTestRunnerService {
	svc := NewScheduledTestRunnerService(planRepo, scheduledSvc, accountTestSvc, rateLimitSvc, cfg)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideOpsScheduledReportService creates and starts OpsScheduledReportService.
func ProvideOpsScheduledReportService(
	opsService *OpsService,
	userService *UserService,
	emailService *EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
	taskExecutor *ClusterTaskExecutor,
) *OpsScheduledReportService {
	svc := NewOpsScheduledReportService(opsService, userService, emailService, redisClient, cfg)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideAPIKeyAuthCacheInvalidator 提供 API Key 认证缓存失效能力
func ProvideAPIKeyAuthCacheInvalidator(apiKeyService *APIKeyService) APIKeyAuthCacheInvalidator {
	// Start Pub/Sub subscriber for L1 cache invalidation across instances
	apiKeyService.StartAuthCacheInvalidationSubscriber(context.Background())
	return apiKeyService
}

func ProvideAPIKeyService(
	apiKeyRepo APIKeyRepository,
	accountShareBindingChecker AccountShareAPIKeyBindingChecker,
	userRepo UserRepository,
	groupRepo GroupRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	usageLogRepo UsageLogRepository,
	cache APIKeyCache,
	cfg *config.Config,
	settingService *SettingService,
	billingCacheService *BillingCacheService,
	concurrencyService *ConcurrencyService,
) *APIKeyService {
	svc := NewAPIKeyService(apiKeyRepo, userRepo, groupRepo, userSubRepo, userGroupRateRepo, cache, cfg)
	svc.SetAccountShareAPIKeyBindingChecker(accountShareBindingChecker)
	svc.SetSettingService(settingService)
	svc.SetRateLimitCacheInvalidator(billingCacheService)
	svc.SetUsageLogRepository(usageLogRepo)
	svc.SetConcurrencyService(concurrencyService)
	return svc
}

// ProvideBackupService creates and starts BackupService
func ProvideBackupService(
	settingRepo SettingRepository,
	cfg *config.Config,
	encryptor SecretEncryptor,
	storeFactory BackupObjectStoreFactory,
	dumper DBDumper,
	taskExecutor *ClusterTaskExecutor,
) *BackupService {
	svc := NewBackupService(settingRepo, cfg, encryptor, storeFactory, dumper)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideSettingService wires SettingService with group reader and proxy repo.
func ProvideSettingService(
	settingRepo SettingRepository,
	groupRepo GroupRepository,
	proxyRepo ProxyRepository,
	cfg *config.Config,
	clusterCache *ClusterCacheCoordinator,
) *SettingService {
	svc := NewSettingService(settingRepo, cfg)
	svc.SetDefaultSubscriptionGroupReader(groupRepo)
	svc.SetProxyRepository(proxyRepo)
	svc.clusterCache = clusterCache
	return svc
}

func ProvideChannelService(
	repo ChannelRepository,
	groupRepo GroupRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	pricingService *PricingService,
	clusterCache *ClusterCacheCoordinator,
) *ChannelService {
	svc := NewChannelService(repo, groupRepo, authCacheInvalidator, pricingService)
	svc.SetClusterCacheCoordinator(clusterCache)
	return svc
}

// ProvideBillingCacheService wires BillingCacheService with its RPM dependencies.
func ProvideBillingCacheService(
	cache BillingCache,
	userRepo UserRepository,
	subRepo UserSubscriptionRepository,
	apiKeyRepo APIKeyRepository,
	rpmCache UserRPMCache,
	rateRepo UserGroupRateRepository,
	cfg *config.Config,
) *BillingCacheService {
	return NewBillingCacheService(cache, userRepo, subRepo, apiKeyRepo, rpmCache, rateRepo, cfg)
}

// ProvideGroupRateScheduleService creates and starts the group rate schedule worker.
func ProvideGroupRateScheduleService(
	repo GroupRateScheduleRepository,
	groupRepo GroupRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	apiKeyRepo APIKeyRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	systemNoticeService *SystemNoticeService,
	taskExecutor *ClusterTaskExecutor,
) *GroupRateScheduleService {
	svc := NewGroupRateScheduleService(repo, groupRepo, authCacheInvalidator, defaultGroupRateScheduleInterval)
	svc.SetNotificationDependencies(apiKeyRepo, userSubRepo, userGroupRateRepo, systemNoticeService)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

func ProvideAffiliateCodeCycleService(
	affiliateService *AffiliateService,
	taskExecutor *ClusterTaskExecutor,
) *AffiliateCodeCycleService {
	svc := NewAffiliateCodeCycleService(affiliateService)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

func ProvideAuthService(
	entClient *dbent.Client,
	userRepo UserRepository,
	redeemRepo RedeemCodeRepository,
	refreshTokenCache RefreshTokenCache,
	cfg *config.Config,
	settingService *SettingService,
	emailService *EmailService,
	turnstileService *TurnstileService,
	emailQueueService *EmailQueueService,
	promoService *PromoService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	affiliateService *AffiliateService,
	privateGroupProvisioner UserPrivateGroupProvisioner,
) *AuthService {
	svc := NewAuthService(
		entClient,
		userRepo,
		redeemRepo,
		refreshTokenCache,
		cfg,
		settingService,
		emailService,
		turnstileService,
		emailQueueService,
		promoService,
		defaultSubAssigner,
		affiliateService,
	)
	svc.SetUserPrivateGroupProvisioner(privateGroupProvisioner)
	return svc
}

func ProvideAccountService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	proxyRepo ProxyRepository,
	accountSharePolicyRepo AccountSharePolicyRepository,
	accountShareModeRepo AccountShareModeRepository,
	privateGroupProvisioner UserPrivateGroupProvisioner,
	concurrencyService *ConcurrencyService,
	systemNoticeService *SystemNoticeService,
	settingService *SettingService,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
	accountShareModeService *AccountShareModeService,
) *AccountService {
	svc := NewAccountService(accountRepo, groupRepo, userRepo, userSubRepo, proxyRepo)
	svc.SetAccountSharePolicyRepository(accountSharePolicyRepo)
	svc.SetAccountShareModeRepository(accountShareModeRepo)
	svc.SetUserPrivateGroupProvisioner(privateGroupProvisioner)
	svc.SetConcurrencyService(concurrencyService)
	svc.SetSystemNoticeService(systemNoticeService)
	svc.SetSettingService(settingService)
	svc.SetAgentIdentityWSInvalidator(agentIdentityWSInvalidator)
	svc.SetAccountShareBillingCacheInvalidator(accountShareModeService)
	return svc
}

func ProvideOpenAIOAuthService(cfg *config.Config, proxyRepo ProxyRepository, oauthClient OpenAIOAuthClient) *OpenAIOAuthService {
	svc := NewOpenAIOAuthService(proxyRepo, oauthClient)
	if cfg != nil {
		svc.SetSessionTokenSecret(cfg.JWT.Secret)
	}
	return svc
}

func ProvideAccountShareModeService(
	cfg *config.Config,
	repo AccountShareModeRepository,
	accountRepo AccountRepository,
	apiKeyRepo APIKeyRepository,
	usageLogRepo UsageLogRepository,
	userRepo UserRepository,
	proxyRepo ProxyRepository,
	openaiOAuthService *OpenAIOAuthService,
	oauthService *OAuthService,
	concurrencyService *ConcurrencyService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	accountTestService *AccountTestService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	billingService *BillingService,
	modelPricingResolver *ModelPricingResolver,
	settingRepo SettingRepository,
	settingService *SettingService,
	taskExecutor *ClusterTaskExecutor,
	billingIntentRepository AccountShareBillingIntentRepository,
	billingIntentWorker *AccountShareBillingWorker,
) *AccountShareModeService {
	svc := NewAccountShareModeService(repo, accountRepo, apiKeyRepo, userRepo, proxyRepo, openaiOAuthService, oauthService)
	if cfg != nil {
		svc.SetActionTokenSecret(cfg.JWT.Secret)
		svc.SetLifecycleContractEnabled(cfg.AccountShareRollout.LifecycleContractEnabled)
	}
	svc.SetRuntimeDependencies(concurrencyService, authCacheInvalidator, accountTestService, rateLimitService)
	svc.SetBillingCacheService(billingCacheService)
	svc.SetRecommendationPricingDependencies(billingService, modelPricingResolver)
	svc.SetSettingService(settingService)
	svc.SetRecommendationUsageProfileRepository(usageLogRepo)
	svc.SetReviewModerationSettingRepository(settingRepo)
	svc.SetBillingIntentRepository(billingIntentRepository)
	svc.SetBillingIntentWorker(billingIntentWorker)
	svc.taskExecutor = taskExecutor
	svc.StartSeatBillingWorker()
	svc.StartReviewModerationWorker()
	return svc
}

func ProvideAccountShareBillingWorker(
	intentRepo AccountShareBillingIntentRepository,
	usageBillingRepo UsageBillingRepository,
	postCommitFinalizer UsageBillingPostCommitFinalizer,
) (*AccountShareBillingWorker, error) {
	return NewAccountShareBillingWorker(intentRepo, usageBillingRepo, postCommitFinalizer, AccountShareBillingWorkerConfig{
		WorkerID: "account-share-billing:" + uuid.NewString(),
	})
}

func ProvideUsageBillingPostCommitFinalizer(
	billingCacheService *BillingCacheService,
	deferredService *DeferredService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	userRepo UserRepository,
	accountRepo AccountRepository,
	balanceNotifyService *BalanceNotifyService,
) (UsageBillingPostCommitFinalizer, error) {
	return NewUsageBillingPostCommitFinalizer(
		billingCacheService,
		deferredService,
		authCacheInvalidator,
		userRepo,
		accountRepo,
		balanceNotifyService,
	)
}

func ProvideAccountShareModeServices(svc *AccountShareModeService) []*AccountShareModeService {
	if svc == nil {
		return nil
	}
	return []*AccountShareModeService{svc}
}

func ProvideGatewayService(
	accountRepo AccountRepository,
	accountSharePolicyRepo AccountSharePolicyRepository,
	groupRepo GroupRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	identityService *IdentityService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	claudeTokenProvider *ClaudeTokenProvider,
	sessionLimitCache SessionLimitCache,
	rpmCache RPMCache,
	digestStore *DigestSessionStore,
	settingService *SettingService,
	tlsFPProfileService *TLSFingerprintProfileService,
	channelService *ChannelService,
	resolver *ModelPricingResolver,
	balanceNotifyService *BalanceNotifyService,
	accountShareModeService *AccountShareModeService,
) *GatewayService {
	return NewGatewayService(
		accountRepo,
		accountSharePolicyRepo,
		groupRepo,
		usageLogRepo,
		usageBillingRepo,
		userRepo,
		userSubRepo,
		userGroupRateRepo,
		cache,
		cfg,
		schedulerSnapshot,
		concurrencyService,
		billingService,
		rateLimitService,
		billingCacheService,
		identityService,
		httpUpstream,
		deferredService,
		claudeTokenProvider,
		sessionLimitCache,
		rpmCache,
		digestStore,
		settingService,
		tlsFPProfileService,
		channelService,
		resolver,
		balanceNotifyService,
		accountShareModeService,
	)
}

func ProvideOpenAIGatewayService(
	accountRepo AccountRepository,
	accountSharePolicyRepo AccountSharePolicyRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	openAITokenProvider *OpenAITokenProvider,
	grokTokenProvider *GrokTokenProvider,
	resolver *ModelPricingResolver,
	channelService *ChannelService,
	balanceNotifyService *BalanceNotifyService,
	settingService *SettingService,
	accountService *AccountService,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
	accountShareModeServices ...*AccountShareModeService,
) *OpenAIGatewayService {
	svc := NewOpenAIGatewayService(
		accountRepo,
		accountSharePolicyRepo,
		usageLogRepo,
		usageBillingRepo,
		userRepo,
		userSubRepo,
		userGroupRateRepo,
		cache,
		cfg,
		schedulerSnapshot,
		concurrencyService,
		billingService,
		rateLimitService,
		billingCacheService,
		httpUpstream,
		deferredService,
		openAITokenProvider,
		resolver,
		channelService,
		balanceNotifyService,
		settingService,
		accountService,
		accountShareModeServices...,
	)
	svc.SetGrokTokenProvider(grokTokenProvider)
	agentIdentityWSInvalidator.SetTarget(svc)
	return svc
}

func ProvideAccountTestService(
	accountRepo AccountRepository,
	geminiTokenProvider *GeminiTokenProvider,
	claudeTokenProvider *ClaudeTokenProvider,
	antigravityGatewayService *AntigravityGatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
	settingService *SettingService,
	grokTokenProvider *GrokTokenProvider,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
) *AccountTestService {
	svc := NewAccountTestService(
		accountRepo,
		geminiTokenProvider,
		claudeTokenProvider,
		antigravityGatewayService,
		httpUpstream,
		cfg,
		tlsFPProfileService,
		settingService,
		agentIdentityWSInvalidator,
	)
	svc.SetGrokTokenProvider(grokTokenProvider)
	return svc
}

func ProvideAccountUsageService(
	accountRepo AccountRepository,
	usageLogRepo UsageLogRepository,
	usageFetcher ClaudeUsageFetcher,
	geminiQuotaService *GeminiQuotaService,
	antigravityQuotaFetcher *AntigravityQuotaFetcher,
	grokQuotaFetcher *GrokQuotaFetcher,
	grokQuotaService *GrokQuotaService,
	cache *UsageCache,
	identityCache IdentityCache,
	tlsFPProfileService *TLSFingerprintProfileService,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
) *AccountUsageService {
	svc := NewAccountUsageService(
		accountRepo,
		usageLogRepo,
		usageFetcher,
		geminiQuotaService,
		antigravityQuotaFetcher,
		cache,
		identityCache,
		tlsFPProfileService,
		grokQuotaFetcher,
	)
	svc.SetAgentIdentityWSInvalidator(agentIdentityWSInvalidator)
	svc.SetGrokQuotaService(grokQuotaService)
	return svc
}

func ProvideSubscriptionService(
	groupRepo GroupRepository,
	userSubRepo UserSubscriptionRepository,
	billingCacheService *BillingCacheService,
	entClient *dbent.Client,
	cfg *config.Config,
	systemNoticeService *SystemNoticeService,
) *SubscriptionService {
	svc := NewSubscriptionService(groupRepo, userSubRepo, billingCacheService, entClient, cfg)
	svc.SetSystemNoticeService(systemNoticeService)
	return svc
}

func ProvideAnnouncementService(
	announcementRepo AnnouncementRepository,
	readRepo AnnouncementReadRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	systemNoticeService *SystemNoticeService,
) *AnnouncementService {
	svc := NewAnnouncementService(announcementRepo, readRepo, userRepo, userSubRepo)
	svc.SetSystemNoticeService(systemNoticeService)
	return svc
}

func ProvideAdminService(
	userRepo UserRepository,
	groupRepo GroupRepository,
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	apiKeyRepo APIKeyRepository,
	accountShareBindingChecker AccountShareAPIKeyBindingChecker,
	redeemCodeRepo RedeemCodeRepository,
	userGroupRateRepo UserGroupRateRepository,
	userRPMCache UserRPMCache,
	billingCacheService *BillingCacheService,
	proxyProber ProxyExitInfoProber,
	proxyLatencyCache ProxyLatencyCache,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	entClient *dbent.Client,
	settingService *SettingService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	userSubRepo UserSubscriptionRepository,
	privacyClientFactory PrivacyClientFactory,
	privateGroupProvisioner UserPrivateGroupProvisioner,
	systemNoticeService *SystemNoticeService,
	agentIdentityWSInvalidator *AgentIdentityWSInvalidatorProxy,
) AdminService {
	svc := NewAdminService(
		userRepo,
		groupRepo,
		accountRepo,
		proxyRepo,
		apiKeyRepo,
		accountShareBindingChecker,
		redeemCodeRepo,
		userGroupRateRepo,
		userRPMCache,
		billingCacheService,
		proxyProber,
		proxyLatencyCache,
		authCacheInvalidator,
		entClient,
		settingService,
		defaultSubAssigner,
		userSubRepo,
		privacyClientFactory,
	)
	svc = SetAdminUserPrivateGroupProvisioner(svc, privateGroupProvisioner)
	svc = SetAdminSystemNoticeService(svc, systemNoticeService)
	return SetAdminAgentIdentityWSInvalidator(svc, agentIdentityWSInvalidator)
}

// ProviderSet is the Wire provider set for all services
var ProviderSet = wire.NewSet(
	// Core services
	ProvideAuthService,
	NewUserService,
	NewOIDCProviderService,
	ProvideAPIKeyService,
	ProvideAPIKeyAuthCacheInvalidator,
	NewGroupService,
	ProvideGroupRateScheduleService,
	ProvideAccountService,
	NewAccountSharePolicyService,
	ProvideAccountShareBillingWorker,
	ProvideUsageBillingPostCommitFinalizer,
	ProvideAccountShareModeService,
	ProvideAccountShareModeServices,
	NewProxyService,
	NewRedeemService,
	NewPromoService,
	NewUsageService,
	NewDashboardService,
	ProvidePricingService,
	NewBillingService,
	ProvideBillingCacheService,
	ProvideAnnouncementService,
	NewConversationService,
	NewSystemNoticeService,
	ProvideAdminService,
	ProvideGatewayService,
	ProvideOpenAIGatewayService,
	NewAgentIdentityWSInvalidatorProxy,
	NewOAuthService,
	ProvideOpenAIOAuthService,
	NewGrokOAuthService,
	NewGeminiOAuthService,
	NewGeminiQuotaService,
	NewCompositeTokenCacheInvalidator,
	wire.Bind(new(TokenCacheInvalidator), new(*CompositeTokenCacheInvalidator)),
	NewAntigravityOAuthService,
	ProvideOAuthRefreshAPI,
	ProvideGeminiTokenProvider,
	NewGeminiMessagesCompatService,
	ProvideAntigravityTokenProvider,
	ProvideGrokTokenProvider,
	ProvideOpenAITokenProvider,
	ProvideOpenAIQuotaService,
	ProvideGrokQuotaService,
	ProvideClaudeTokenProvider,
	NewAntigravityGatewayService,
	ProvideRateLimitService,
	ProvideAccountUsageService,
	ProvideAccountTestService,
	ProvideSettingService,
	NewDataManagementService,
	ProvideBackupService,
	ProvideOpsSystemLogSink,
	NewOpsService,
	ProvideOpsMetricsCollector,
	ProvideOpsAggregationService,
	ProvideOpsAlertEvaluatorService,
	ProvideOpsCleanupService,
	ProvideOpsScheduledReportService,
	NewEmailService,
	ProvideEmailQueueService,
	NewTurnstileService,
	ProvideSubscriptionService,
	wire.Bind(new(DefaultSubscriptionAssigner), new(*SubscriptionService)),
	NewUserPrivateGroupService,
	ProvideConcurrencyService,
	ProvideUserMessageQueueService,
	NewUsageRecordWorkerPool,
	ProvideSchedulerSnapshotService,
	NewIdentityService,
	NewCRSSyncService,
	ProvideUpdateService,
	ProvideTokenRefreshService,
	wire.Bind(new(GrokOAuthReconciler), new(*TokenRefreshService)),
	ProvideAccountExpiryService,
	ProvideAccountErrorCleanupService,
	ProvideSubscriptionExpiryService,
	ProvideTimingWheelService,
	ProvideDashboardAggregationService,
	ProvideUsageCleanupService,
	ProvideAccountBatchTaskService,
	ProvideAccountBatchTaskServices,
	ProvideDeferredService,
	NewAntigravityQuotaFetcher,
	NewGrokQuotaFetcher,
	NewUserAttributeService,
	NewUsageCache,
	NewTotpService,
	NewErrorPassthroughService,
	NewTLSFingerprintProfileService,
	NewDigestSessionStore,
	ProvideIdempotencyCoordinator,
	ProvideSystemOperationLockService,
	ProvideIdempotencyCleanupService,
	ProvideScheduledTestService,
	ProvideScheduledTestRunnerService,
	NewGroupCapacityService,
	ProvideChannelService,
	NewClusterConnectionTracker,
	NewClusterNodeState,
	NewClusterCacheCoordinator,
	NewClusterService,
	NewClusterRuntime,
	NewClusterTaskExecutor,
	NewModelPricingResolver,
	ProvideContentModerationService,
	NewUserContentModerationService,
	NewAffiliateService,
	ProvideAffiliateCodeCycleService,
	NewRevenueService,
	NewReceiptCodeService,
	NewWithdrawalService,
	NewInvoiceService,
	ProvideShopService,
	NewActivityService,
	ProvideActivityAutoDrawService,
	ProvidePaymentConfigService,
	wire.Bind(new(ReceiptCodeStorageConfigProvider), new(*PaymentConfigService)),
	ProvidePaymentService,
	ProvidePaymentOrderExpiryService,
	ProvideBalanceNotifyService,
	ProvideChannelMonitorService,
	ProvideChannelMonitorRunner,
	NewChannelMonitorRequestTemplateService,
)

// ProvidePaymentConfigService wraps NewPaymentConfigService to accept the named
// payment.EncryptionKey type instead of raw []byte, avoiding Wire ambiguity.
func ProvidePaymentConfigService(entClient *dbent.Client, settingRepo SettingRepository, key payment.EncryptionKey, cfg *config.Config) *PaymentConfigService {
	return NewPaymentConfigService(entClient, settingRepo, []byte(key), cfg)
}

func ProvidePaymentService(
	entClient *dbent.Client,
	registry *payment.Registry,
	loadBalancer payment.LoadBalancer,
	redeemService *RedeemService,
	subscriptionSvc *SubscriptionService,
	configService *PaymentConfigService,
	userRepo UserRepository,
	groupRepo GroupRepository,
	affiliateService *AffiliateService,
	systemNoticeService *SystemNoticeService,
) *PaymentService {
	svc := NewPaymentService(entClient, registry, loadBalancer, redeemService, subscriptionSvc, configService, userRepo, groupRepo, affiliateService)
	svc.SetSystemNoticeService(systemNoticeService)
	return svc
}

func ProvideContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	accountShareModeService *AccountShareModeService,
	userContentModerationRepo UserContentModerationRepository,
	userRepo UserRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
	systemNoticeService *SystemNoticeService,
	clusterCache *ClusterCacheCoordinator,
	taskExecutor *ClusterTaskExecutor,
) *ContentModerationService {
	svc := NewContentModerationService(settingRepo, repo, hashCache, groupRepo, userRepo, authCacheInvalidator, emailService, taskExecutor)
	svc.SetAccountShareModeResolver(accountShareModeService)
	svc.SetUserAPIKeyHashChecker(userContentModerationRepo)
	svc.SetSystemNoticeService(systemNoticeService)
	svc.SetClusterCacheCoordinator(clusterCache)
	return svc
}

func ProvideShopService(
	entClient *dbent.Client,
	paymentService *PaymentService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	billingCacheService *BillingCacheService,
	settingRepo SettingRepository,
	key payment.EncryptionKey,
	fileCardStoreFactory ShopFileCardObjectStoreFactory,
) *ShopService {
	svc := NewShopService(
		entClient,
		paymentService,
		authCacheInvalidator,
		billingCacheService,
		WithShopSettingRepository(settingRepo),
		WithShopEncryptionKey([]byte(key)),
		WithShopFileCardObjectStoreFactory(fileCardStoreFactory),
	)
	paymentService.SetShopFulfillment(svc)
	return svc
}

// ProvideBalanceNotifyService creates BalanceNotifyService
func ProvideBalanceNotifyService(emailService *EmailService, settingRepo SettingRepository, accountRepo AccountRepository) *BalanceNotifyService {
	return NewBalanceNotifyService(emailService, settingRepo, accountRepo)
}

// ProvidePaymentOrderExpiryService creates and starts PaymentOrderExpiryService.
func ProvidePaymentOrderExpiryService(
	paymentSvc *PaymentService,
	taskExecutor *ClusterTaskExecutor,
) *PaymentOrderExpiryService {
	svc := NewPaymentOrderExpiryService(paymentSvc, 60*time.Second)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}

// ProvideChannelMonitorService 创建渠道监控服务（CRUD + RunCheck + 用户视图聚合）。
// 加密器复用 wire 中已注入的 SecretEncryptor（AES-256-GCM）。
func ProvideChannelMonitorService(
	repo ChannelMonitorRepository,
	encryptor SecretEncryptor,
) *ChannelMonitorService {
	return NewChannelMonitorService(repo, encryptor)
}

// ProvideChannelMonitorRunner 创建并启动渠道监控调度器。
// 通过 SetScheduler 注入回 service 后再 Start，确保启动时加载所有 enabled monitor，
// 后续 CRUD 也能即时同步任务表。Runner.Stop 由 cleanup function 调用。
// settingService 用于 runner 每次 fire 读取功能开关。
func ProvideChannelMonitorRunner(
	svc *ChannelMonitorService,
	settingService *SettingService,
	taskExecutor *ClusterTaskExecutor,
) *ChannelMonitorRunner {
	r := NewChannelMonitorRunner(svc, settingService, taskExecutor)
	svc.SetScheduler(r)
	r.Start()
	return r
}
