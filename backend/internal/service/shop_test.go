package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestShopPlatformFulfillmentUsesReservedCards(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := NewShopService(client, nil, nil, nil)
	user := createShopTestUser(t, ctx, client, "reserved@example.com")
	product := createShopTestProduct(t, ctx, client, "Reserved product")
	cardA := createShopTestCard(t, ctx, client, product.ID, "CARD-A")
	cardB := createShopTestCard(t, ctx, client, product.ID, "CARD-B")
	order := createShopTestOrder(t, ctx, client, user.ID, product.ID, "platform", ShopOrderStatusPending, 1)
	paymentOrder := createShopTestPaymentOrder(t, ctx, client, user.ID, order.ID, order.TotalAmount, payment.OrderStatusPaid)
	order, err := client.ShopOrder.UpdateOneID(order.ID).SetPaymentOrderID(paymentOrder.ID).Save(ctx)
	require.NoError(t, err)
	now := time.Now()
	cardA, err = client.ShopCardKey.UpdateOneID(cardA.ID).
		SetStatus(ShopCardStatusLocked).
		SetOrderID(order.ID).
		SetLockedAt(now).
		SetLockedUntil(now.Add(30 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.ConfirmPaidAndDeliver(ctx, paymentOrder.ID))

	cardA, err = client.ShopCardKey.Get(ctx, cardA.ID)
	require.NoError(t, err)
	cardB, err = client.ShopCardKey.Get(ctx, cardB.ID)
	require.NoError(t, err)
	require.Equal(t, ShopCardStatusSold, cardA.Status)
	require.Equal(t, ShopCardStatusAvailable, cardB.Status)
	require.Nil(t, cardA.LockedAt)
	require.Nil(t, cardA.LockedUntil)
	fulfilled, err := client.ShopOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, ShopOrderStatusCompleted, fulfilled.Status)
	require.Equal(t, []string{"CARD-A"}, fulfilled.DeliveredCards)
}

func TestShopCancelKeepsReservationUntilGraceCleanup(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := NewShopService(client, nil, nil, nil)
	user := createShopTestUser(t, ctx, client, "cancel@example.com")
	product := createShopTestProduct(t, ctx, client, "Cancel product")
	card := createShopTestCard(t, ctx, client, product.ID, "CARD-C")
	order := createShopTestOrder(t, ctx, client, user.ID, product.ID, "platform", ShopOrderStatusPending, 1)
	paymentOrder := createShopTestPaymentOrder(t, ctx, client, user.ID, order.ID, order.TotalAmount, payment.OrderStatusPending)
	order, err := client.ShopOrder.UpdateOneID(order.ID).SetPaymentOrderID(paymentOrder.ID).Save(ctx)
	require.NoError(t, err)
	lockedUntil := time.Now().Add(-10 * time.Minute)
	card, err = client.ShopCardKey.UpdateOneID(card.ID).
		SetStatus(ShopCardStatusLocked).
		SetOrderID(order.ID).
		SetLockedAt(lockedUntil.Add(-30 * time.Minute)).
		SetLockedUntil(lockedUntil).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.CancelPendingPayment(ctx, paymentOrder.ID, ShopOrderStatusCancelled))
	card, err = client.ShopCardKey.Get(ctx, card.ID)
	require.NoError(t, err)
	require.Equal(t, ShopCardStatusLocked, card.Status)
	require.NotNil(t, card.OrderID)

	require.NoError(t, svc.ReleaseStalePaymentReservations(ctx, time.Now()))
	card, err = client.ShopCardKey.Get(ctx, card.ID)
	require.NoError(t, err)
	require.Equal(t, ShopCardStatusAvailable, card.Status)
	require.Nil(t, card.OrderID)
	require.Nil(t, card.LockedAt)
	require.Nil(t, card.LockedUntil)
}

func TestShopFulfillmentRejectsPaymentUserMismatch(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := NewShopService(client, nil, nil, nil)
	user := createShopTestUser(t, ctx, client, "buyer@example.com")
	other := createShopTestUser(t, ctx, client, "other@example.com")
	product := createShopTestProduct(t, ctx, client, "Mismatch product")
	createShopTestCard(t, ctx, client, product.ID, "CARD-D")
	order := createShopTestOrder(t, ctx, client, user.ID, product.ID, "platform", ShopOrderStatusPending, 1)
	paymentOrder := createShopTestPaymentOrder(t, ctx, client, other.ID, order.ID, order.TotalAmount, payment.OrderStatusPaid)
	_, err := client.ShopOrder.UpdateOneID(order.ID).SetPaymentOrderID(paymentOrder.ID).Save(ctx)
	require.NoError(t, err)

	err = svc.ConfirmPaidAndDeliver(ctx, paymentOrder.ID)
	require.Error(t, err)
	require.Equal(t, "SHOP_PAYMENT_USER_MISMATCH", errorCodeForTest(err))
}

func TestAdminCannotUpdateOrDeleteLockedCardKey(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := NewShopService(client, nil, nil, nil)
	user := createShopTestUser(t, ctx, client, "locked-admin@example.com")
	product := createShopTestProduct(t, ctx, client, "Locked product")
	card := createShopTestCard(t, ctx, client, product.ID, "CARD-E")
	order := createShopTestOrder(t, ctx, client, user.ID, product.ID, "platform", ShopOrderStatusPending, 1)
	card, err := client.ShopCardKey.UpdateOneID(card.ID).
		SetStatus(ShopCardStatusLocked).
		SetOrderID(order.ID).
		SetLockedAt(time.Now()).
		SetLockedUntil(time.Now().Add(30 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	newContent := "CARD-E-EDITED"
	_, err = svc.AdminUpdateCardKey(ctx, card.ID, ShopUpdateCardKeyRequest{Content: &newContent})
	require.Error(t, err)
	require.Equal(t, "SHOP_CARD_KEY_ALREADY_ASSIGNED", errorCodeForTest(err))

	err = svc.AdminDeleteCardKey(ctx, card.ID)
	require.Error(t, err)
	require.Equal(t, "SHOP_CARD_KEY_ALREADY_ASSIGNED", errorCodeForTest(err))
}

func TestShopPlatformFulfillmentReallocatesAvailableCardAfterReservationReleased(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := NewShopService(client, nil, nil, nil)
	user := createShopTestUser(t, ctx, client, "released@example.com")
	other := createShopTestUser(t, ctx, client, "released-other@example.com")
	product := createShopTestProduct(t, ctx, client, "Released reservation product")
	oldCard := createShopTestCard(t, ctx, client, product.ID, "CARD-F")
	newCard := createShopTestCard(t, ctx, client, product.ID, "CARD-G")
	otherOrder := createShopTestOrder(t, ctx, client, other.ID, product.ID, "platform", ShopOrderStatusCompleted, 1)
	oldCard, err := client.ShopCardKey.UpdateOneID(oldCard.ID).
		SetStatus(ShopCardStatusSold).
		SetOrderID(otherOrder.ID).
		SetSoldAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)
	order := createShopTestOrder(t, ctx, client, user.ID, product.ID, "platform", ShopOrderStatusFailed, 1)
	paymentOrder := createShopTestPaymentOrder(t, ctx, client, user.ID, order.ID, order.TotalAmount, payment.OrderStatusPaid)
	_, err = client.ShopOrder.UpdateOneID(order.ID).
		SetPaymentOrderID(paymentOrder.ID).
		SetFailedReason("reservation released").
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.ConfirmPaidAndDeliver(ctx, paymentOrder.ID))

	oldCard, err = client.ShopCardKey.Get(ctx, oldCard.ID)
	require.NoError(t, err)
	require.Equal(t, ShopCardStatusSold, oldCard.Status)
	require.NotNil(t, oldCard.OrderID)
	require.Equal(t, otherOrder.ID, *oldCard.OrderID)
	newCard, err = client.ShopCardKey.Get(ctx, newCard.ID)
	require.NoError(t, err)
	require.Equal(t, ShopCardStatusSold, newCard.Status)
	require.NotNil(t, newCard.OrderID)
	require.Equal(t, order.ID, *newCard.OrderID)
	fulfilled, err := client.ShopOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, ShopOrderStatusCompleted, fulfilled.Status)
	require.Equal(t, []string{"CARD-G"}, fulfilled.DeliveredCards)
}

func TestAdminImportFileCardKeysRollsBackBatchOnUploadFailure(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensureShopFileCardTestColumns(t, ctx, client)
	product := createShopTestProduct(t, ctx, client, "File import rollback product")
	store := newMemoryShopFileCardStore()
	store.failUploadAt = 2
	svc := NewShopService(
		client,
		nil,
		nil,
		nil,
		WithShopSettingRepository(newShopFileCardTestSettingRepo()),
		WithShopFileCardObjectStoreFactory(func(context.Context, ShopFileCardStorageConfig) (ShopFileCardObjectStore, error) {
			return store, nil
		}),
	)

	_, err := svc.AdminImportFileCardKeys(ctx, product.ID, []ShopFileCardUpload{
		{Filename: "first.txt", ContentType: "text/plain", Reader: bytes.NewReader([]byte("first card"))},
		{Filename: "second.txt", ContentType: "text/plain", Reader: bytes.NewReader([]byte("second card"))},
	})
	require.Error(t, err)
	require.Empty(t, store.objects)
	require.Len(t, store.deleted, 1)
	count, err := client.ShopCardKey.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestAdminDeleteFileCardKeyDeletesObjectAfterDatabaseCommit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensureShopFileCardTestColumns(t, ctx, client)
	product := createShopTestProduct(t, ctx, client, "File delete product")
	card := createShopTestFileCard(t, ctx, client, product.ID, "cards/delete-me.txt")
	store := newMemoryShopFileCardStore()
	store.objects["cards/delete-me.txt"] = []byte("file body")
	store.deleteHook = func(key string) error {
		_, err := client.ShopCardKey.Get(ctx, card.ID)
		if err == nil {
			return errors.New("file object deleted before database row")
		}
		if !dbent.IsNotFound(err) {
			return err
		}
		return nil
	}
	svc := NewShopService(
		client,
		nil,
		nil,
		nil,
		WithShopSettingRepository(newShopFileCardTestSettingRepo()),
		WithShopFileCardObjectStoreFactory(func(context.Context, ShopFileCardStorageConfig) (ShopFileCardObjectStore, error) {
			return store, nil
		}),
	)

	require.NoError(t, svc.AdminDeleteCardKey(ctx, card.ID))
	_, err := client.ShopCardKey.Get(ctx, card.ID)
	require.True(t, dbent.IsNotFound(err))
	require.Equal(t, []string{"cards/delete-me.txt"}, store.deleted)
	require.Empty(t, store.objects)
}

func TestWriteOrderFileCardArchiveStreamsDeliveredFiles(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensureShopFileCardTestColumns(t, ctx, client)
	user := createShopTestUser(t, ctx, client, "file-archive@example.com")
	product := createShopTestProduct(t, ctx, client, "File archive product")
	order := createShopTestOrder(t, ctx, client, user.ID, product.ID, ShopPaymentMethodBalance, ShopOrderStatusCompleted, 2)
	cardA := createShopTestFileCard(t, ctx, client, product.ID, "cards/a.txt")
	cardB := createShopTestFileCard(t, ctx, client, product.ID, "cards/b.txt")
	now := time.Now()
	for _, card := range []*dbent.ShopCardKey{cardA, cardB} {
		_, err := client.ShopCardKey.UpdateOneID(card.ID).
			SetStatus(ShopCardStatusSold).
			SetOrderID(order.ID).
			SetSoldAt(now).
			Save(ctx)
		require.NoError(t, err)
	}
	store := newMemoryShopFileCardStore()
	store.objects["cards/a.txt"] = []byte("alpha")
	store.objects["cards/b.txt"] = []byte("beta")
	svc := NewShopService(
		client,
		nil,
		nil,
		nil,
		WithShopSettingRepository(newShopFileCardTestSettingRepo()),
		WithShopFileCardObjectStoreFactory(func(context.Context, ShopFileCardStorageConfig) (ShopFileCardObjectStore, error) {
			return store, nil
		}),
	)

	var buf bytes.Buffer
	filename, err := svc.WriteOrderFileCardArchive(ctx, user.ID, order.ID, &buf)
	require.NoError(t, err)
	require.Equal(t, "shop-order-"+strconv.FormatInt(order.ID, 10)+"-files.zip", filename)
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 2)
	require.Equal(t, "file.txt", zr.File[0].Name)
	require.Equal(t, "file-2.txt", zr.File[1].Name)
}

func createShopTestUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("hash").
		SetUsername(email).
		Save(ctx)
	require.NoError(t, err)
	return user
}

func createShopTestProduct(t *testing.T, ctx context.Context, client *dbent.Client, name string) *dbent.ShopProduct {
	t.Helper()
	product, err := client.ShopProduct.Create().
		SetName(name).
		SetPrice(12.34).
		SetEnabled(true).
		SetMinPurchase(1).
		SetMaxPurchase(10).
		SetAutoDelivery(true).
		Save(ctx)
	require.NoError(t, err)
	return product
}

func createShopTestCard(t *testing.T, ctx context.Context, client *dbent.Client, productID int64, content string) *dbent.ShopCardKey {
	t.Helper()
	card, err := client.ShopCardKey.Create().
		SetProductID(productID).
		SetContent(content).
		SetStatus(ShopCardStatusAvailable).
		Save(ctx)
	require.NoError(t, err)
	return card
}

func createShopTestFileCard(t *testing.T, ctx context.Context, client *dbent.Client, productID int64, storageKey string) *dbent.ShopCardKey {
	t.Helper()
	card := createShopTestCard(t, ctx, client, productID, "file-placeholder")
	execShopTestSQL(t, ctx, client, `
		UPDATE shop_card_keys
		SET card_type = $1,
			storage_provider = $2,
			storage_key = $3,
			original_filename = $4,
			content_type = $5,
			byte_size = $6,
			sha256 = $7
		WHERE id = $8
	`, ShopCardTypeFile, ShopFileCardStorageProviderOSS, storageKey, "file.txt", "text/plain", 5, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", card.ID)
	return card
}

func ensureShopFileCardTestColumns(t *testing.T, ctx context.Context, client *dbent.Client) {
	t.Helper()
	statements := []string{
		"ALTER TABLE shop_card_keys ADD COLUMN card_type varchar(20) NOT NULL DEFAULT 'text'",
		"ALTER TABLE shop_card_keys ADD COLUMN storage_provider varchar(20)",
		"ALTER TABLE shop_card_keys ADD COLUMN storage_key text",
		"ALTER TABLE shop_card_keys ADD COLUMN original_filename varchar(255)",
		"ALTER TABLE shop_card_keys ADD COLUMN content_type varchar(120)",
		"ALTER TABLE shop_card_keys ADD COLUMN byte_size integer",
		"ALTER TABLE shop_card_keys ADD COLUMN sha256 varchar(64)",
	}
	for _, statement := range statements {
		execShopTestSQL(t, ctx, client, statement)
	}
}

func execShopTestSQL(t *testing.T, ctx context.Context, client *dbent.Client, query string, args ...any) {
	t.Helper()
	drv, ok := client.Driver().(*entsql.Driver)
	require.True(t, ok, "test client must use ent sql driver")
	_, err := drv.DB().ExecContext(ctx, query, args...)
	require.NoError(t, err)
}

func newShopFileCardTestSettingRepo() *paymentConfigSettingRepoStub {
	return &paymentConfigSettingRepoStub{values: map[string]string{
		settingShopFileCardOSSEnabled:         "true",
		settingShopFileCardOSSEndpoint:        "https://oss.example.com",
		settingShopFileCardOSSRegion:          "oss-cn-hangzhou",
		settingShopFileCardOSSBucket:          "shop-file-cards",
		settingShopFileCardOSSAccessKeyID:     "access-key",
		settingShopFileCardOSSSecretAccessKey: `{"secret":"secret-key"}`,
		settingShopFileCardOSSPrefix:          "cards/",
		settingShopFileCardOSSForcePathStyle:  "false",
	}}
}

type memoryShopFileCardStore struct {
	objects      map[string][]byte
	deleted      []string
	failUploadAt int
	uploadCount  int
	deleteHook   func(string) error
}

func newMemoryShopFileCardStore() *memoryShopFileCardStore {
	return &memoryShopFileCardStore{objects: map[string][]byte{}}
}

func (s *memoryShopFileCardStore) Upload(_ context.Context, key string, body io.Reader, _ string) error {
	s.uploadCount++
	if s.failUploadAt > 0 && s.uploadCount == s.failUploadAt {
		return errors.New("upload failed")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.objects[key] = data
	return nil
}

func (s *memoryShopFileCardStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *memoryShopFileCardStore) Delete(_ context.Context, key string) error {
	if s.deleteHook != nil {
		if err := s.deleteHook(key); err != nil {
			return err
		}
	}
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}

func (s *memoryShopFileCardStore) HeadBucket(context.Context) error {
	return nil
}

func createShopTestOrder(t *testing.T, ctx context.Context, client *dbent.Client, userID, productID int64, paymentMethod, status string, quantity int) *dbent.ShopOrder {
	t.Helper()
	total := normalizeShopAmount(12.34 * float64(quantity))
	order, err := client.ShopOrder.Create().
		SetOrderNo("SHOPTEST" + generateRandomString(12)).
		SetUserID(userID).
		SetProductID(productID).
		SetProductName("test product").
		SetUnitPrice(12.34).
		SetQuantity(quantity).
		SetTotalAmount(total).
		SetPaymentMethod(paymentMethod).
		SetStatus(status).
		SetDeliveredCards([]string{}).
		Save(ctx)
	require.NoError(t, err)
	return order
}

func createShopTestPaymentOrder(t *testing.T, ctx context.Context, client *dbent.Client, userID, shopOrderID int64, amount float64, status string) *dbent.PaymentOrder {
	t.Helper()
	paymentOrder, err := client.PaymentOrder.Create().
		SetUserID(userID).
		SetUserEmail("payment@example.com").
		SetUserName("payment-user").
		SetAmount(amount).
		SetPayAmount(amount).
		SetFeeRate(0).
		SetRechargeCode("PAY-" + generateRandomString(8)).
		SetOutTradeNo("sub2_test_" + generateRandomString(12)).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeShop).
		SetShopOrderID(shopOrderID).
		SetStatus(status).
		SetExpiresAt(time.Now().Add(30 * time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)
	return paymentOrder
}

func errorCodeForTest(err error) string {
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Reason
	}
	return ""
}
