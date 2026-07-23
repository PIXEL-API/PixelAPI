//go:build unit

package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ─── Mocks ───

type mockSettingRepo struct {
	mu             sync.Mutex
	data           map[string]string
	getValueErrors map[string]error
	setErrors      map[string]error
}

func newMockSettingRepo() *mockSettingRepo {
	return &mockSettingRepo{
		data:           make(map[string]string),
		getValueErrors: make(map[string]error),
		setErrors:      make(map[string]error),
	}
}

func (m *mockSettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v}, nil
}

func (m *mockSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.getValueErrors[key]; err != nil {
		return "", err
	}
	v, ok := m.data[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (m *mockSettingRepo) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.setErrors[key]; err != nil {
		return err
	}
	m.data[key] = value
	return nil
}

func (m *mockSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			result[k] = v
		}
	}
	return result, nil
}

func (m *mockSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range settings {
		m.data[k] = v
	}
	return nil
}

func (m *mockSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string, len(m.data))
	for k, v := range m.data {
		result[k] = v
	}
	return result, nil
}

func (m *mockSettingRepo) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// plainEncryptor 仅做 base64-like 包装，用于测试
type plainEncryptor struct{}

func (e *plainEncryptor) Encrypt(plaintext string) (string, error) {
	return "ENC:" + plaintext, nil
}

func (e *plainEncryptor) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, "ENC:") {
		return strings.TrimPrefix(ciphertext, "ENC:"), nil
	}
	return ciphertext, fmt.Errorf("not encrypted")
}

type mockDumper struct {
	dumpData []byte
	dumpErr  error
	restored []byte
	restErr  error
}

func (m *mockDumper) Dump(_ context.Context) (io.ReadCloser, error) {
	if m.dumpErr != nil {
		return nil, m.dumpErr
	}
	return io.NopCloser(bytes.NewReader(m.dumpData)), nil
}

func (m *mockDumper) Restore(_ context.Context, data io.Reader) error {
	if m.restErr != nil {
		return m.restErr
	}
	d, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	m.restored = d
	return nil
}

// blockingDumper 可控延迟的 dumper，用于测试异步行为
type blockingDumper struct {
	blockCh chan struct{}
	data    []byte
	restErr error
}

func (d *blockingDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	select {
	case <-d.blockCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return io.NopCloser(bytes.NewReader(d.data)), nil
}

func (d *blockingDumper) Restore(_ context.Context, data io.Reader) error {
	if d.restErr != nil {
		return d.restErr
	}
	_, _ = io.ReadAll(data)
	return nil
}

type mockObjectStore struct {
	objects           map[string][]byte
	uploadHook        func()
	deleteErr         error
	deleteErrors      map[string]error
	deleteCalls       []string
	deleteBlock       <-chan struct{}
	deleteStarted     chan struct{}
	deleteStartedOnce sync.Once
	mu                sync.Mutex
}

func newMockObjectStore() *mockObjectStore {
	return &mockObjectStore{
		objects:      make(map[string][]byte),
		deleteErrors: make(map[string]error),
	}
}

func (m *mockObjectStore) Upload(_ context.Context, key string, body io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.objects[key] = data
	hook := m.uploadHook
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	return int64(len(data)), nil
}

func (m *mockObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	data, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockObjectStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, key)
	deleteErr := m.deleteErrors[key]
	if deleteErr == nil {
		deleteErr = m.deleteErr
	}
	deleteBlock := m.deleteBlock
	deleteStarted := m.deleteStarted
	m.mu.Unlock()

	if deleteStarted != nil {
		m.deleteStartedOnce.Do(func() { close(deleteStarted) })
	}
	if deleteBlock != nil {
		select {
		case <-deleteBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if deleteErr != nil {
		return deleteErr
	}

	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

func (m *mockObjectStore) PresignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://presigned.example.com/" + key, nil
}

func (m *mockObjectStore) HeadBucket(_ context.Context) error {
	return nil
}

func newTestBackupService(repo *mockSettingRepo, dumper DBDumper, store *mockObjectStore) *BackupService {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Host:   "localhost",
			Port:   5432,
			User:   "test",
			DBName: "testdb",
		},
		Totp: config.TotpConfig{EncryptionKeyConfigured: true},
	}
	factory := func(_ context.Context, _ *BackupS3Config) (BackupObjectStore, error) {
		return store, nil
	}
	return NewBackupService(repo, cfg, &plainEncryptor{}, factory, dumper)
}

func newTestBackupServiceWithEphemeralKey(repo *mockSettingRepo) *BackupService {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Host:   "localhost",
			Port:   5432,
			User:   "test",
			DBName: "testdb",
		},
		Totp: config.TotpConfig{EncryptionKeyConfigured: false},
	}
	return NewBackupService(repo, cfg, &plainEncryptor{}, func(_ context.Context, _ *BackupS3Config) (BackupObjectStore, error) {
		return newMockObjectStore(), nil
	}, &mockDumper{})
}

func seedS3Config(t *testing.T, repo *mockSettingRepo) {
	t.Helper()
	cfg := BackupS3Config{
		Bucket:          "test-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "ENC:secret123",
		Prefix:          "backups",
	}
	data, _ := json.Marshal(cfg)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupS3Config, string(data)))
}

func seedBackupRecord(t *testing.T, svc *BackupService, store *mockObjectStore, record BackupRecord) {
	t.Helper()
	require.NoError(t, svc.saveRecord(context.Background(), &record))
	if record.S3Key == "" {
		return
	}
	store.mu.Lock()
	store.objects[record.S3Key] = []byte(record.ID)
	store.mu.Unlock()
}

// ─── Tests ───

func TestBackupService_S3ConfigEncryption(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	// 保存配置 -> SecretAccessKey 应被加密
	_, err := svc.UpdateS3Config(context.Background(), BackupS3Config{
		Bucket:          "my-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "my-secret",
		Prefix:          "backups",
	})
	require.NoError(t, err)

	// 直接读取数据库中存储的值，应该是加密后的
	raw, _ := repo.GetValue(context.Background(), settingKeyBackupS3Config)
	var stored BackupS3Config
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, backupEncryptedSecretV1+"ENC:my-secret", stored.SecretAccessKey)

	// 通过 GetS3Config 获取应该脱敏
	cfg, err := svc.GetS3Config(context.Background())
	require.NoError(t, err)
	require.Empty(t, cfg.SecretAccessKey)
	require.Equal(t, "my-bucket", cfg.Bucket)

	// loadS3Config 内部应解密
	internal, err := svc.loadS3Config(context.Background())
	require.NoError(t, err)
	require.Equal(t, "my-secret", internal.SecretAccessKey)
}

func TestBackupService_S3ConfigRejectsEphemeralEncryptionKey(t *testing.T) {
	svc := newTestBackupServiceWithEphemeralKey(newMockSettingRepo())

	_, err := svc.UpdateS3Config(context.Background(), BackupS3Config{
		Bucket:          "my-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "my-secret",
	})
	require.ErrorIs(t, err, ErrSecretEncryptionKeyNotConfigured)
	require.False(t, svc.EncryptionKeyConfigured())
}

func TestBackupService_S3ConfigKeepExistingSecret(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	// 先保存一个有 secret 的配置
	_, err := svc.UpdateS3Config(context.Background(), BackupS3Config{
		Bucket:          "my-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "original-secret",
	})
	require.NoError(t, err)

	// 再更新时不提供 secret，应保留原值
	_, err = svc.UpdateS3Config(context.Background(), BackupS3Config{
		Bucket:      "my-bucket",
		AccessKeyID: "AKID-NEW",
	})
	require.NoError(t, err)

	internal, err := svc.loadS3Config(context.Background())
	require.NoError(t, err)
	require.Equal(t, "original-secret", internal.SecretAccessKey)
	require.Equal(t, "AKID-NEW", internal.AccessKeyID)

	raw, err := repo.GetValue(context.Background(), settingKeyBackupS3Config)
	require.NoError(t, err)
	var stored BackupS3Config
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, backupEncryptedSecretV1+"ENC:original-secret", stored.SecretAccessKey)
}

func TestBackupService_S3ConfigFailsFastWhenVersionedSecretCannotBeDecrypted(t *testing.T) {
	repo := newMockSettingRepo()
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupS3Config,
		`{"bucket":"my-bucket","access_key_id":"AKID","secret_access_key":"enc:v1:broken"}`))
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	_, err := svc.loadS3Config(context.Background())

	require.ErrorIs(t, err, ErrBackupS3ConfigCorrupt)
}

func TestBackupService_UpdateS3ConfigRejectsStorageLocationChangesWithRecords(t *testing.T) {
	base := BackupS3Config{
		Endpoint:        "https://objects.example.com",
		Region:          "auto",
		Bucket:          "archive-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "secret",
		Prefix:          "backups",
		ForcePathStyle:  false,
	}
	tests := []struct {
		name   string
		mutate func(*BackupS3Config)
	}{
		{name: "endpoint", mutate: func(cfg *BackupS3Config) { cfg.Endpoint = "https://other.example.com" }},
		{name: "region", mutate: func(cfg *BackupS3Config) { cfg.Region = "us-east-1" }},
		{name: "bucket", mutate: func(cfg *BackupS3Config) { cfg.Bucket = "other-bucket" }},
		{name: "prefix", mutate: func(cfg *BackupS3Config) { cfg.Prefix = "other-prefix" }},
		{name: "path_style", mutate: func(cfg *BackupS3Config) { cfg.ForcePathStyle = true }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockSettingRepo()
			store := newMockObjectStore()
			svc := newTestBackupService(repo, &mockDumper{}, store)
			_, err := svc.UpdateS3Config(context.Background(), base)
			require.NoError(t, err)
			seedBackupRecord(t, svc, store, BackupRecord{
				ID:         "existing",
				Status:     "completed",
				BackupType: "usage_logs_archive",
				S3Key:      "backups/2026/07/18/existing.gz",
				StartedAt:  time.Now().Format(time.RFC3339),
			})

			updated := base
			updated.SecretAccessKey = ""
			tt.mutate(&updated)
			_, err = svc.UpdateS3Config(context.Background(), updated)
			require.Error(t, err)
			require.Equal(t, "BACKUP_STORAGE_LOCATION_IN_USE", infraerrors.Reason(err))
		})
	}
}

func TestBackupService_UpdateS3ConfigAllowsCredentialRotationWithRecords(t *testing.T) {
	repo := newMockSettingRepo()
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	base := BackupS3Config{
		Endpoint:        "https://objects.example.com/",
		Region:          "auto",
		Bucket:          "archive-bucket",
		AccessKeyID:     "OLD-AKID",
		SecretAccessKey: "old-secret",
		Prefix:          "backups",
	}
	_, err := svc.UpdateS3Config(context.Background(), base)
	require.NoError(t, err)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "existing",
		Status:     "completed",
		BackupType: "ops_error_logs_archive",
		S3Key:      "backups/2026/07/18/existing.gz",
		StartedAt:  time.Now().Format(time.RFC3339),
	})

	updated := base
	updated.Endpoint = "https://objects.example.com"
	updated.Prefix = "backups/"
	updated.AccessKeyID = "NEW-AKID"
	updated.SecretAccessKey = "new-secret"
	_, err = svc.UpdateS3Config(context.Background(), updated)
	require.NoError(t, err)
	stored, err := svc.loadS3Config(context.Background())
	require.NoError(t, err)
	require.Equal(t, "NEW-AKID", stored.AccessKeyID)
	require.Equal(t, "new-secret", stored.SecretAccessKey)
}

func TestBackupService_UpdateS3ConfigRejectsNewLocationWhenConfigWasRemoved(t *testing.T) {
	repo := newMockSettingRepo()
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "orphaned-location",
		Status:     "completed",
		BackupType: "postgres",
		S3Key:      "backups/orphaned",
		StartedAt:  time.Now().Format(time.RFC3339),
	})

	_, err := svc.UpdateS3Config(context.Background(), BackupS3Config{
		Endpoint:        "https://objects.example.com",
		Region:          "auto",
		Bucket:          "archive-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "secret",
		Prefix:          "backups",
	})
	require.Error(t, err)
	require.Equal(t, "BACKUP_STORAGE_LOCATION_IN_USE", infraerrors.Reason(err))
}

func TestBackupService_SaveRecordConcurrency(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	var wg sync.WaitGroup
	n := 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			record := &BackupRecord{
				ID:        fmt.Sprintf("rec-%d", idx),
				Status:    "completed",
				StartedAt: time.Now().Format(time.RFC3339),
			}
			_ = svc.saveRecord(context.Background(), record)
		}(i)
	}
	wg.Wait()

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Len(t, records, n)
}

func TestBackupService_SaveRecordRetainsObjectReferences(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	const recordCount = 101
	for i := 0; i < recordCount; i++ {
		require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
			ID:        fmt.Sprintf("retained-%d", i),
			Status:    "completed",
			S3Key:     fmt.Sprintf("backups/retained-%d", i),
			StartedAt: time.Now().Format(time.RFC3339),
		}))
	}

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Len(t, records, recordCount)
	require.Equal(t, "retained-0", records[0].ID)
	require.Equal(t, "retained-100", records[len(records)-1].ID)
}

func TestBackupService_LoadRecords_Empty(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Nil(t, records) // 无数据时返回 nil
}

func TestBackupService_LoadRecords_Corrupted(t *testing.T) {
	repo := newMockSettingRepo()
	_ = repo.Set(context.Background(), settingKeyBackupRecords, "not valid json{{{")
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	records, err := svc.loadRecords(context.Background())
	require.Error(t, err) // 损坏数据应返回错误
	require.Nil(t, records)
}

func TestBackupService_LoadRecords_EmptyValueIsCorrupt(t *testing.T) {
	repo := newMockSettingRepo()
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupRecords, ""))
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	records, err := svc.loadRecords(context.Background())
	require.ErrorIs(t, err, ErrBackupRecordsCorrupt)
	require.Nil(t, records)
}

func TestBackupService_LoadRecords_RepositoryErrorPropagates(t *testing.T) {
	repo := newMockSettingRepo()
	repoErr := fmt.Errorf("temporary setting repository failure")
	repo.getValueErrors[settingKeyBackupRecords] = repoErr
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	records, err := svc.loadRecords(context.Background())
	require.ErrorIs(t, err, repoErr)
	require.Nil(t, records)
}

func TestBackupService_SaveRecordLoadFailureDoesNotOverwriteRecords(t *testing.T) {
	repo := newMockSettingRepo()
	original := []BackupRecord{{ID: "original", Status: "completed", S3Key: "backups/original"}}
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupRecords, string(raw)))
	repoErr := fmt.Errorf("temporary setting repository failure")
	repo.getValueErrors[settingKeyBackupRecords] = repoErr
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	err = svc.saveRecord(context.Background(), &BackupRecord{ID: "new", Status: "completed"})
	require.ErrorIs(t, err, repoErr)
	repo.mu.Lock()
	require.JSONEq(t, string(raw), repo.data[settingKeyBackupRecords])
	repo.mu.Unlock()
}

func TestBackupService_CreateBackup_Streaming(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumpContent := "-- PostgreSQL dump\nCREATE TABLE test (id int);\n"
	dumper := &mockDumper{dumpData: []byte(dumpContent)}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)
	require.Equal(t, "completed", record.Status)
	require.Greater(t, record.SizeBytes, int64(0))
	require.NotEmpty(t, record.S3Key)

	// 验证 S3 上确实有文件
	store.mu.Lock()
	require.Len(t, store.objects, 1)
	store.mu.Unlock()
}

func TestBackupServiceCreateUsageLogsArchive(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	record, err := svc.CreateUsageLogsArchive(context.Background(), UsageLogsArchiveInput{
		Stream:     io.NopCloser(strings.NewReader("{\"id\":1}\n")),
		StartTime:  start,
		EndTime:    end,
		ExpireDays: 7,
	})
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "completed", record.Status)
	require.Equal(t, "usage_logs_archive", record.BackupType)
	require.Contains(t, record.FileName, "usage_logs_")
	require.Contains(t, record.FileName, ".ndjson.gz")
	require.NotEmpty(t, record.ExpiresAt)

	store.mu.Lock()
	uploaded := append([]byte(nil), store.objects[record.S3Key]...)
	store.mu.Unlock()
	gz, err := gzip.NewReader(bytes.NewReader(uploaded))
	require.NoError(t, err)
	defer func() { _ = gz.Close() }()
	plain, err := io.ReadAll(gz)
	require.NoError(t, err)
	require.Equal(t, "{\"id\":1}\n", string(plain))
}

func TestBackupService_CreateDataArchiveSaveFailureDeletesUploadedObject(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	persistErr := fmt.Errorf("backup record storage unavailable")
	repo.setErrors[settingKeyBackupRecords] = persistErr
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)

	record, err := svc.CreateDataArchive(context.Background(), DataArchiveInput{
		Stream:      io.NopCloser(strings.NewReader("{\"id\":1}\n")),
		FileName:    "usage_logs.ndjson.gz",
		BackupType:  "usage_logs_archive",
		TriggeredBy: "usage_cleanup_auto",
		ExpireDays:  180,
	})
	require.ErrorIs(t, err, persistErr)
	require.NotNil(t, record)
	require.Equal(t, "failed", record.Status)
	store.mu.Lock()
	require.Empty(t, store.objects)
	require.Equal(t, []string{record.S3Key}, store.deleteCalls)
	store.mu.Unlock()
}

func TestBackupService_CreateDataArchiveCompensationFailureJoinsErrors(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	persistErr := fmt.Errorf("backup record storage unavailable")
	repo.setErrors[settingKeyBackupRecords] = persistErr
	compensationErr := fmt.Errorf("object storage delete unavailable")
	store := newMockObjectStore()
	store.deleteErr = compensationErr
	svc := newTestBackupService(repo, &mockDumper{}, store)

	record, err := svc.CreateDataArchive(context.Background(), DataArchiveInput{
		Stream:      io.NopCloser(strings.NewReader("{\"id\":1}\n")),
		FileName:    "usage_logs.ndjson.gz",
		BackupType:  "usage_logs_archive",
		TriggeredBy: "usage_cleanup_auto",
		ExpireDays:  180,
	})
	require.ErrorIs(t, err, persistErr)
	require.ErrorIs(t, err, compensationErr)
	require.NotNil(t, record)
	require.Equal(t, "failed", record.Status)
	store.mu.Lock()
	require.Contains(t, store.objects, record.S3Key)
	require.Equal(t, []string{record.S3Key}, store.deleteCalls)
	store.mu.Unlock()
}

func TestBackupService_CreateDataArchiveDoesNotInlineCleanupExpiredObjects(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "expired-existing",
		Status:     "completed",
		BackupType: "usage_logs_archive",
		S3Key:      "expired/existing",
		StartedAt:  now.Add(-48 * time.Hour).Format(time.RFC3339),
		ExpiresAt:  now.Add(-time.Hour).Format(time.RFC3339),
	})

	_, err := svc.CreateDataArchive(context.Background(), DataArchiveInput{
		Stream:      io.NopCloser(strings.NewReader("{\"id\":2}\n")),
		FileName:    "new.ndjson.gz",
		BackupType:  "usage_logs_archive",
		TriggeredBy: "usage_cleanup_auto",
		ExpireDays:  180,
	})
	require.NoError(t, err)
	store.mu.Lock()
	require.Empty(t, store.deleteCalls)
	require.Contains(t, store.objects, "expired/existing")
	store.mu.Unlock()
}

func TestBackupServiceRestoreRejectsUsageLogsArchive(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
		ID:         "archive1",
		Status:     "completed",
		BackupType: "usage_logs_archive",
		S3Key:      "backups/archive.ndjson.gz",
		StartedAt:  time.Now().Format(time.RFC3339),
	}))

	err := svc.RestoreBackup(context.Background(), "archive1")
	require.Error(t, err)
	require.Equal(t, "BACKUP_TYPE_NOT_RESTORABLE", infraerrors.Reason(err))
}

func TestBackupService_CreateBackup_DumpFailure(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumper := &mockDumper{dumpErr: fmt.Errorf("pg_dump failed")}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.Error(t, err)
	require.Equal(t, "failed", record.Status)
	require.Contains(t, record.ErrorMsg, "pg_dump")
}

func TestBackupService_CreateBackup_NoS3Config(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	_, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, ErrBackupS3NotConfigured)
}

func TestBackupService_CreateBackup_ConcurrentBlocked(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	// 使用一个慢速 dumper 来模拟正在进行的备份
	dumper := &mockDumper{dumpData: []byte("data")}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	// 手动设置 backingUp 标志
	svc.opMu.Lock()
	svc.backingUp = true
	svc.opMu.Unlock()

	_, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, ErrBackupInProgress)
}

func TestBackupService_BackupAndArchiveRejectedDuringRestore(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	svc.opMu.Lock()
	svc.restoring = true
	svc.opMu.Unlock()

	_, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, ErrRestoreInProgress)
	_, err = svc.StartBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, ErrRestoreInProgress)
	_, err = svc.CreateDataArchive(context.Background(), DataArchiveInput{
		Stream:     io.NopCloser(strings.NewReader("{}\n")),
		FileName:   "archive.ndjson.gz",
		BackupType: "usage_logs_archive",
	})
	require.ErrorIs(t, err, ErrRestoreInProgress)
	svc.wg.Wait()
}

func TestBackupService_RestoreRejectedDuringBackup(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	svc.opMu.Lock()
	svc.backingUp = true
	svc.opMu.Unlock()

	err := svc.RestoreBackup(context.Background(), "backup-id")
	require.ErrorIs(t, err, ErrBackupInProgress)
	_, err = svc.StartRestore(context.Background(), "backup-id")
	require.ErrorIs(t, err, ErrBackupInProgress)
	svc.wg.Wait()
}

func TestBackupService_RestoreBackup_Streaming(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumpContent := "-- PostgreSQL dump\nCREATE TABLE test (id int);\n"
	dumper := &mockDumper{dumpData: []byte(dumpContent)}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	// 先创建一个备份
	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)

	// 恢复
	err = svc.RestoreBackup(context.Background(), record.ID)
	require.NoError(t, err)

	// 验证 psql 收到的数据是否与原始 dump 内容一致
	require.Equal(t, dumpContent, string(dumper.restored))
}

func TestBackupService_RestoreBackup_NotCompleted(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	// 手动插入一条 failed 记录
	_ = svc.saveRecord(context.Background(), &BackupRecord{
		ID:     "fail-1",
		Status: "failed",
	})

	err := svc.RestoreBackup(context.Background(), "fail-1")
	require.Error(t, err)
}

func TestBackupService_DeleteBackup(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumpContent := "data"
	dumper := &mockDumper{dumpData: []byte(dumpContent)}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)

	// S3 中应有文件
	store.mu.Lock()
	require.Len(t, store.objects, 1)
	store.mu.Unlock()

	// 删除
	err = svc.DeleteBackup(context.Background(), record.ID)
	require.NoError(t, err)

	// S3 中文件应被删除
	store.mu.Lock()
	require.Len(t, store.objects, 0)
	store.mu.Unlock()

	// 记录应不存在
	_, err = svc.GetBackupRecord(context.Background(), record.ID)
	require.ErrorIs(t, err, ErrBackupNotFound)
}

func TestBackupService_GetDownloadURL(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumper := &mockDumper{dumpData: []byte("data")}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)

	url, err := svc.GetBackupDownloadURL(context.Background(), record.ID)
	require.NoError(t, err)
	require.Contains(t, url, "https://presigned.example.com/")
}

func TestBackupService_ListBackups_Sorted(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	now := time.Now()
	for i := 0; i < 3; i++ {
		_ = svc.saveRecord(context.Background(), &BackupRecord{
			ID:        fmt.Sprintf("rec-%d", i),
			Status:    "completed",
			StartedAt: now.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		})
	}

	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 3)
	// 最新在前
	require.Equal(t, "rec-2", records[0].ID)
	require.Equal(t, "rec-0", records[2].ID)
}

func TestBackupService_TestS3Connection(t *testing.T) {
	repo := newMockSettingRepo()
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)

	err := svc.TestS3Connection(context.Background(), BackupS3Config{
		Bucket:          "test",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
}

func TestBackupService_TestS3Connection_Incomplete(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	err := svc.TestS3Connection(context.Background(), BackupS3Config{
		Bucket: "test",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "incomplete")
}

func TestBackupService_Schedule_CronValidation(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	svc.cronSched = nil // 未初始化 cron

	// 启用但 cron 为空
	_, err := svc.UpdateSchedule(context.Background(), BackupScheduleConfig{
		Enabled:  true,
		CronExpr: "",
	})
	require.Error(t, err)

	// 无效的 cron 表达式
	_, err = svc.UpdateSchedule(context.Background(), BackupScheduleConfig{
		Enabled:  true,
		CronExpr: "invalid",
	})
	require.Error(t, err)
}

func TestBackupService_StartRegistersExpirationCleanupWhenFullBackupDisabled(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	scheduleData, err := json.Marshal(BackupScheduleConfig{Enabled: false})
	require.NoError(t, err)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, string(scheduleData)))
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "expired-before-start",
		Status:     "completed",
		BackupType: "usage_logs_archive",
		S3Key:      "expired/before-start",
		StartedAt:  now.Add(-2 * time.Hour).Format(time.RFC3339),
		ExpiresAt:  now.Add(-time.Hour).Format(time.RFC3339),
	})

	svc.Start()
	defer svc.Stop()

	svc.cronMu.Lock()
	cleanupEntryID := svc.cronCleanupEntryID
	backupEntryID := svc.cronEntryID
	entries := svc.cronSched.Entries()
	svc.cronMu.Unlock()
	require.NotZero(t, cleanupEntryID)
	require.Zero(t, backupEntryID)
	require.Len(t, entries, 1)
	require.Equal(t, cleanupEntryID, entries[0].ID)
	require.Equal(t, 3, entries[0].Next.Hour())
	require.Equal(t, 30, entries[0].Next.Minute())

	// Start 只注册固定低峰任务，不立即执行删除。
	_, err = svc.GetBackupRecord(context.Background(), "expired-before-start")
	require.NoError(t, err)
	store.mu.Lock()
	require.Empty(t, store.deleteCalls)
	store.mu.Unlock()
}

func TestBackupService_StopDisablesExpirationCleanupLifecycle(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "expired-after-stop",
		Status:     "completed",
		BackupType: "ops_error_logs_archive",
		S3Key:      "expired/after-stop",
		StartedAt:  now.Add(-2 * time.Hour).Format(time.RFC3339),
		ExpiresAt:  now.Add(-time.Hour).Format(time.RFC3339),
	})

	svc.Start()
	svc.Stop()
	require.True(t, svc.shuttingDown.Load())
	svc.cronMu.Lock()
	require.Zero(t, svc.cronCleanupEntryID)
	require.Zero(t, svc.cronEntryID)
	svc.cronMu.Unlock()

	// 即使停止后出现迟到的定时回调，也不得进入清理或增加 WaitGroup。
	svc.runScheduledExpirationCleanup()
	svc.wg.Wait()
	_, err := svc.GetBackupRecord(context.Background(), "expired-after-stop")
	require.NoError(t, err)
	store.mu.Lock()
	require.Empty(t, store.deleteCalls)
	store.mu.Unlock()
}

func TestScheduledExpirationCleanup_BusyRetriesAreBounded(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "expired-during-backup",
		Status:     "completed",
		BackupType: "ops_system_logs_archive",
		S3Key:      "expired/during-backup",
		StartedAt:  now.Add(-2 * time.Hour).Format(time.RFC3339),
		ExpiresAt:  now.Add(-time.Hour).Format(time.RFC3339),
	})
	svc.opMu.Lock()
	svc.backingUp = true
	svc.opMu.Unlock()

	err := svc.cleanupExpiredBackupsWithRetry(context.Background(), 3, time.Millisecond)
	require.ErrorIs(t, err, ErrBackupInProgress)
	_, err = svc.GetBackupRecord(context.Background(), "expired-during-backup")
	require.NoError(t, err)
	store.mu.Lock()
	require.Empty(t, store.deleteCalls)
	store.mu.Unlock()
}

func TestScheduledExpirationCleanup_RetriesAfterBackupFinishes(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "expired-after-busy",
		Status:     "completed",
		BackupType: "ops_system_logs_archive",
		S3Key:      "expired/after-busy",
		StartedAt:  now.Add(-2 * time.Hour).Format(time.RFC3339),
		ExpiresAt:  now.Add(-time.Hour).Format(time.RFC3339),
	})
	svc.opMu.Lock()
	svc.backingUp = true
	svc.opMu.Unlock()
	time.AfterFunc(5*time.Millisecond, func() {
		svc.opMu.Lock()
		svc.backingUp = false
		svc.opMu.Unlock()
	})

	err := svc.cleanupExpiredBackupsWithRetry(context.Background(), 2, 20*time.Millisecond)
	require.NoError(t, err)
	_, err = svc.GetBackupRecord(context.Background(), "expired-after-busy")
	require.ErrorIs(t, err, ErrBackupNotFound)
	store.mu.Lock()
	require.Equal(t, []string{"expired/after-busy"}, store.deleteCalls)
	store.mu.Unlock()
}

func TestBackupService_LoadS3Config_Corrupted(t *testing.T) {
	repo := newMockSettingRepo()
	_ = repo.Set(context.Background(), settingKeyBackupS3Config, "not json!!!!")
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	cfg, err := svc.loadS3Config(context.Background())
	require.Error(t, err)
	require.Nil(t, cfg)
}

func TestBackupService_LoadS3Config_RepositoryErrorPropagates(t *testing.T) {
	repo := newMockSettingRepo()
	repoErr := fmt.Errorf("temporary setting repository failure")
	repo.getValueErrors[settingKeyBackupS3Config] = repoErr
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	cfg, err := svc.loadS3Config(context.Background())
	require.ErrorIs(t, err, repoErr)
	require.Nil(t, cfg)
	err = svc.TestS3Connection(context.Background(), BackupS3Config{Bucket: "bucket", AccessKeyID: "AKID"})
	require.ErrorIs(t, err, repoErr)
}

// ─── Async Backup Tests ───

func TestStartBackup_ReturnsImmediately(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumper := &blockingDumper{blockCh: make(chan struct{}), data: []byte("data")}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	record, err := svc.StartBackup(context.Background(), "manual", 14)
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)
	require.NotEmpty(t, record.ID)

	// 释放 dumper 让后台完成
	close(dumper.blockCh)
	svc.wg.Wait()

	// 验证最终状态
	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", final.Status)
	require.Greater(t, final.SizeBytes, int64(0))
}

func TestStartBackup_ConcurrentBlocked(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumper := &blockingDumper{blockCh: make(chan struct{}), data: []byte("data")}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	// 第一次启动
	_, err := svc.StartBackup(context.Background(), "manual", 14)
	require.NoError(t, err)

	// 第二次应被阻塞
	_, err = svc.StartBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, ErrBackupInProgress)

	close(dumper.blockCh)
	svc.wg.Wait()
}

func TestStartBackup_ShuttingDown(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("data")}, newMockObjectStore())

	svc.shuttingDown.Store(true)

	_, err := svc.StartBackup(context.Background(), "manual", 14)
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutting down")
}

func TestRecoverStaleRecords(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())

	// 模拟一条孤立的 running 记录
	_ = svc.saveRecord(context.Background(), &BackupRecord{
		ID:        "stale-1",
		Status:    "running",
		StartedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})
	// 模拟一条孤立的恢复中记录
	_ = svc.saveRecord(context.Background(), &BackupRecord{
		ID:            "stale-2",
		Status:        "completed",
		RestoreStatus: "running",
		StartedAt:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})

	svc.recoverStaleRecords()

	r1, _ := svc.GetBackupRecord(context.Background(), "stale-1")
	require.Equal(t, "failed", r1.Status)
	require.Contains(t, r1.ErrorMsg, "server restart")

	r2, _ := svc.GetBackupRecord(context.Background(), "stale-2")
	require.Equal(t, "failed", r2.RestoreStatus)
	require.Contains(t, r2.RestoreError, "server restart")
}

func TestGracefulShutdown(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumper := &blockingDumper{blockCh: make(chan struct{}), data: []byte("data")}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	record, err := svc.StartBackup(context.Background(), "manual", 14)
	require.NoError(t, err)

	// Stop 先取消后台 context，再有界等待 goroutine 自行收口；无需手工释放 dumper。
	done := make(chan struct{})
	go func() {
		svc.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(dumper.blockCh)
		t.Fatal("Stop did not cancel and finish the active backup within the bounded wait")
	}
	close(dumper.blockCh)
	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", final.Status)
	require.Contains(t, final.ErrorMsg, "context canceled")
}

func TestBackupService_StopRejectsNewOperations(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("data")}, store)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "existing",
		Status:     "completed",
		BackupType: "postgres",
		S3Key:      "backups/existing",
		StartedAt:  time.Now().Format(time.RFC3339),
	})
	svc.Stop()

	assertShuttingDown := func(err error) {
		t.Helper()
		require.Error(t, err)
		require.Equal(t, "SERVER_SHUTTING_DOWN", infraerrors.Reason(err))
	}
	_, err := svc.CreateBackup(context.Background(), "manual", 14)
	assertShuttingDown(err)
	_, err = svc.StartBackup(context.Background(), "manual", 14)
	assertShuttingDown(err)
	_, err = svc.CreateDataArchive(context.Background(), DataArchiveInput{
		Stream:     io.NopCloser(strings.NewReader("{}\n")),
		FileName:   "archive.ndjson.gz",
		BackupType: "usage_logs_archive",
	})
	assertShuttingDown(err)
	err = svc.RestoreBackup(context.Background(), "existing")
	assertShuttingDown(err)
	_, err = svc.StartRestore(context.Background(), "existing")
	assertShuttingDown(err)
	err = svc.DeleteBackup(context.Background(), "existing")
	assertShuttingDown(err)
	_, err = svc.UpdateS3Config(context.Background(), BackupS3Config{})
	assertShuttingDown(err)
}

func TestBackupService_StopSerializesWaitGroupRegistration(t *testing.T) {
	svc := newTestBackupService(newMockSettingRepo(), &mockDumper{}, newMockObjectStore())
	start := make(chan struct{})
	var callers sync.WaitGroup
	for i := 0; i < 100; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			if svc.tryBeginRun() {
				time.Sleep(time.Microsecond)
				svc.endRun()
			}
		}()
	}
	stopDone := make(chan struct{})
	go func() {
		<-start
		svc.Stop()
		close(stopDone)
	}()
	close(start)
	callers.Wait()
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not finish after registered operations ended")
	}
	require.False(t, svc.tryBeginRun())
}

func TestStartBackup_InitialRecordFailureBalancesLifecycle(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	persistErr := fmt.Errorf("backup record storage unavailable")
	repo.setErrors[settingKeyBackupRecords] = persistErr
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("data")}, newMockObjectStore())

	_, err := svc.StartBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, persistErr)
	svc.opMu.Lock()
	require.False(t, svc.backingUp)
	svc.opMu.Unlock()
	waitDone := make(chan struct{})
	go func() {
		svc.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("StartBackup leaked its lifecycle registration after initialization failure")
	}
}

func TestStartBackup_FinalRecordFailureCompensatesUploadedObject(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	persistErr := fmt.Errorf("backup record storage unavailable")
	store := newMockObjectStore()
	store.uploadHook = func() {
		repo.mu.Lock()
		repo.setErrors[settingKeyBackupRecords] = persistErr
		repo.mu.Unlock()
	}
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("data")}, store)

	record, err := svc.StartBackup(context.Background(), "manual", 14)
	require.NoError(t, err)
	svc.wg.Wait()
	repo.mu.Lock()
	delete(repo.setErrors, settingKeyBackupRecords)
	repo.mu.Unlock()
	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	// 初始 running 记录存在；最终状态保存失败时不能伪装为 completed，且上传对象已补偿删除。
	require.Equal(t, "running", final.Status)
	store.mu.Lock()
	require.Empty(t, store.objects)
	require.Equal(t, []string{record.S3Key}, store.deleteCalls)
	store.mu.Unlock()
}

func TestStartRestore_StatusSaveFailureBalancesLifecycle(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID:         "restore-source",
		Status:     "completed",
		BackupType: "postgres",
		S3Key:      "backups/restore-source",
		StartedAt:  time.Now().Format(time.RFC3339),
	})
	persistErr := fmt.Errorf("backup record storage unavailable")
	repo.setErrors[settingKeyBackupRecords] = persistErr

	_, err := svc.StartRestore(context.Background(), "restore-source")
	require.ErrorIs(t, err, persistErr)
	svc.opMu.Lock()
	require.False(t, svc.restoring)
	svc.opMu.Unlock()
	waitDone := make(chan struct{})
	go func() {
		svc.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("StartRestore leaked its lifecycle registration after initialization failure")
	}
}

func TestStartRestore_Async(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)

	dumpContent := "-- PostgreSQL dump\nCREATE TABLE test (id int);\n"
	dumper := &mockDumper{dumpData: []byte(dumpContent)}
	store := newMockObjectStore()
	svc := newTestBackupService(repo, dumper, store)

	// 先创建一个备份（同步方式）
	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)

	// 异步恢复
	restored, err := svc.StartRestore(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "running", restored.RestoreStatus)

	svc.wg.Wait()

	// 验证最终状态
	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", final.RestoreStatus)
}

func TestCleanupExpiredBackups_ArchiveTypesOnly(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)

	for _, record := range []BackupRecord{
		{ID: "expired-usage", Status: "completed", BackupType: "usage_logs_archive", S3Key: "expired/usage", StartedAt: now.Add(-48 * time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{ID: "expired-ops-system", Status: "completed", BackupType: "ops_system_logs_archive", S3Key: "expired/ops-system", StartedAt: now.Add(-48 * time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{ID: "expired-postgres", Status: "completed", BackupType: "postgres", S3Key: "expired/postgres", StartedAt: now.Add(-48 * time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{ID: "future-ops-error", Status: "completed", BackupType: "ops_error_logs_archive", S3Key: "future/ops-error", StartedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)},
		{ID: "never-expires", Status: "completed", BackupType: "usage_logs_archive", S3Key: "future/never", StartedAt: now.Format(time.RFC3339)},
	} {
		seedBackupRecord(t, svc, store, record)
	}

	require.NoError(t, svc.cleanupExpiredBackups(context.Background()))
	for _, id := range []string{"expired-usage", "expired-ops-system"} {
		_, err := svc.GetBackupRecord(context.Background(), id)
		require.ErrorIs(t, err, ErrBackupNotFound)
	}
	for _, id := range []string{"expired-postgres", "future-ops-error", "never-expires"} {
		_, err := svc.GetBackupRecord(context.Background(), id)
		require.NoError(t, err)
	}
	store.mu.Lock()
	require.ElementsMatch(t, []string{"expired/usage", "expired/ops-system"}, store.deleteCalls)
	require.Len(t, store.objects, 3)
	store.mu.Unlock()
}

func TestCleanupExpiredBackups_FailuresRemainAndAreReported(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	expired := now.Add(-time.Hour).Format(time.RFC3339)

	for _, record := range []BackupRecord{
		{ID: "deleted", Status: "completed", BackupType: "usage_logs_archive", S3Key: "expired/deleted", StartedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: expired},
		{ID: "delete-failed", Status: "completed", BackupType: "ops_error_logs_archive", S3Key: "expired/failure", StartedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: expired},
		{ID: "invalid-expiry", Status: "completed", BackupType: "usage_logs_archive", S3Key: "expired/invalid", StartedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: "invalid"},
		{ID: "empty-key", Status: "completed", BackupType: "ops_system_logs_archive", StartedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: expired},
	} {
		seedBackupRecord(t, svc, store, record)
	}
	store.mu.Lock()
	store.deleteErrors["expired/failure"] = fmt.Errorf("S3 unavailable")
	store.mu.Unlock()

	err := svc.cleanupExpiredBackups(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete-failed")
	require.Contains(t, err.Error(), "invalid-expiry")
	require.Contains(t, err.Error(), "empty-key")
	_, err = svc.GetBackupRecord(context.Background(), "deleted")
	require.ErrorIs(t, err, ErrBackupNotFound)
	for _, id := range []string{"delete-failed", "invalid-expiry", "empty-key"} {
		_, err = svc.GetBackupRecord(context.Background(), id)
		require.NoError(t, err)
	}
}

func TestCleanupExpiredBackups_DeletesAtMostOneBatch(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < backupExpirationCleanupBatchSize+1; i++ {
		seedBackupRecord(t, svc, store, BackupRecord{
			ID:         fmt.Sprintf("expired-%d", i),
			Status:     "completed",
			BackupType: "usage_logs_archive",
			S3Key:      fmt.Sprintf("expired/%d", i),
			StartedAt:  now.Add(-48 * time.Hour).Format(time.RFC3339),
			ExpiresAt:  now.Add(-time.Hour).Format(time.RFC3339),
		})
	}

	require.NoError(t, svc.cleanupExpiredBackups(context.Background()))
	store.mu.Lock()
	require.Len(t, store.deleteCalls, backupExpirationCleanupBatchSize)
	store.mu.Unlock()
	records, err := svc.loadRecords(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestCleanupOldBackups_PostgresOnly(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)

	for _, record := range []BackupRecord{
		{ID: "pg-new", Status: "completed", BackupType: "postgres", S3Key: "pg/new", StartedAt: now.Format(time.RFC3339)},
		{ID: "pg-old", Status: "completed", BackupType: "postgres", S3Key: "pg/old", StartedAt: now.AddDate(0, 0, -60).Format(time.RFC3339)},
		{ID: "legacy-pg-old", Status: "completed", S3Key: "pg/legacy", StartedAt: now.AddDate(0, 0, -90).Format(time.RFC3339)},
		{ID: "usage-old", Status: "completed", BackupType: "usage_logs_archive", S3Key: "archive/usage", StartedAt: now.AddDate(0, 0, -90).Format(time.RFC3339)},
		{ID: "ops-old", Status: "completed", BackupType: "ops_error_logs_archive", S3Key: "archive/ops", StartedAt: now.AddDate(0, 0, -90).Format(time.RFC3339)},
	} {
		seedBackupRecord(t, svc, store, record)
	}

	require.NoError(t, svc.cleanupOldBackups(context.Background(), &BackupScheduleConfig{RetainDays: 30, RetainCount: 1}))
	for _, id := range []string{"pg-old", "legacy-pg-old"} {
		_, err := svc.GetBackupRecord(context.Background(), id)
		require.ErrorIs(t, err, ErrBackupNotFound)
	}
	for _, id := range []string{"pg-new", "usage-old", "ops-old"} {
		_, err := svc.GetBackupRecord(context.Background(), id)
		require.NoError(t, err)
	}
	store.mu.Lock()
	require.ElementsMatch(t, []string{"pg/old", "pg/legacy"}, store.deleteCalls)
	store.mu.Unlock()
}

func TestCleanupOldBackups_AllPostgresBackupsExpiredKeepsLatest(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		seedBackupRecord(t, svc, store, BackupRecord{
			ID:         fmt.Sprintf("pg-%d", i),
			Status:     "completed",
			BackupType: "postgres",
			S3Key:      fmt.Sprintf("pg/%d", i),
			StartedAt:  now.AddDate(0, 0, -(90 - i)).Format(time.RFC3339),
		})
	}

	require.NoError(t, svc.cleanupOldBackups(context.Background(), &BackupScheduleConfig{RetainDays: 30, RetainCount: 0}))
	_, err := svc.GetBackupRecord(context.Background(), "pg-2")
	require.NoError(t, err)
	for _, id := range []string{"pg-0", "pg-1"} {
		_, err = svc.GetBackupRecord(context.Background(), id)
		require.ErrorIs(t, err, ErrBackupNotFound)
	}
	store.mu.Lock()
	require.ElementsMatch(t, []string{"pg/0", "pg/1"}, store.deleteCalls)
	store.mu.Unlock()
}

func TestCleanupExpiredBackups_BlocksConcurrentBackup(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	deleteBlock := make(chan struct{})
	store.deleteBlock = deleteBlock
	store.deleteStarted = make(chan struct{})
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("data")}, store)
	now := time.Now().UTC().Truncate(time.Second)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID: "expired", Status: "completed", BackupType: "usage_logs_archive", S3Key: "expired/object",
		StartedAt: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339),
	})

	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- svc.cleanupExpiredBackups(context.Background()) }()
	select {
	case <-store.deleteStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expiration cleanup did not reach object deletion")
	}
	_, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.ErrorIs(t, err, ErrBackupInProgress)
	close(deleteBlock)
	require.NoError(t, <-cleanupDone)
}

func TestDeleteBackup_DeleteFailureKeepsRecord(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	svc := newTestBackupService(repo, &mockDumper{}, store)
	seedBackupRecord(t, svc, store, BackupRecord{
		ID: "keep-on-error", Status: "completed", BackupType: "postgres", S3Key: "delete/failure", StartedAt: time.Now().Format(time.RFC3339),
	})
	store.mu.Lock()
	store.deleteErrors["delete/failure"] = fmt.Errorf("S3 unavailable")
	store.mu.Unlock()

	err := svc.DeleteBackup(context.Background(), "keep-on-error")
	require.Error(t, err)
	_, getErr := svc.GetBackupRecord(context.Background(), "keep-on-error")
	require.NoError(t, getErr)
}
