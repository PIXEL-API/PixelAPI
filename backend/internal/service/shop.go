package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/shopcardkey"
	"github.com/Wei-Shaw/sub2api/ent/shopcategory"
	"github.com/Wei-Shaw/sub2api/ent/shoporder"
	"github.com/Wei-Shaw/sub2api/ent/shopproduct"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	entsql "entgo.io/ent/dialect/sql"
)

const (
	ShopCardStatusAvailable = "available"
	ShopCardStatusLocked    = "locked"
	ShopCardStatusSold      = "sold"
	ShopCardStatusDisabled  = "disabled"

	ShopCardTypeText = "text"
	ShopCardTypeFile = "file"

	ShopFileCardStorageProviderOSS = "oss"
	ShopFileCardMaxSizeBytes       = int64(200 * 1024)

	ShopOrderStatusPending   = "pending"
	ShopOrderStatusPaid      = "paid"
	ShopOrderStatusCompleted = "completed"
	ShopOrderStatusCancelled = "cancelled"
	ShopOrderStatusFailed    = "failed"

	ShopPaymentMethodBalance = "balance"
)

const shopAmountTolerance = 0.01

var (
	ErrShopProductNotFound          = infraerrors.NotFound("SHOP_PRODUCT_NOT_FOUND", "shop product not found")
	ErrShopCategoryNotFound         = infraerrors.NotFound("SHOP_CATEGORY_NOT_FOUND", "shop category not found")
	ErrShopOrderNotFound            = infraerrors.NotFound("SHOP_ORDER_NOT_FOUND", "shop order not found")
	ErrShopCardKeyNotFound          = infraerrors.NotFound("SHOP_CARD_KEY_NOT_FOUND", "shop card key not found")
	ErrShopInvalidInput             = infraerrors.BadRequest("SHOP_INVALID_INPUT", "invalid shop input")
	ErrShopProductUnavailable       = infraerrors.Forbidden("SHOP_PRODUCT_UNAVAILABLE", "shop product is unavailable")
	ErrShopInvalidQuantity          = infraerrors.BadRequest("SHOP_INVALID_QUANTITY", "invalid purchase quantity")
	ErrShopInsufficientStock        = infraerrors.Conflict("SHOP_INSUFFICIENT_STOCK", "insufficient shop stock")
	ErrShopInsufficientBalance      = infraerrors.Forbidden("SHOP_INSUFFICIENT_BALANCE", "insufficient balance")
	ErrShopUnsupportedPayment       = infraerrors.BadRequest("SHOP_UNSUPPORTED_PAYMENT_METHOD", "unsupported shop payment method")
	ErrShopInvalidOrderStatus       = infraerrors.Conflict("SHOP_INVALID_ORDER_STATUS", "invalid shop order status")
	ErrShopPaymentAmountMismatch    = infraerrors.Conflict("SHOP_PAYMENT_AMOUNT_MISMATCH", "shop payment amount mismatch")
	ErrShopAutoDeliveryRequired     = infraerrors.BadRequest("SHOP_AUTO_DELIVERY_REQUIRED", "product does not support automatic delivery")
	ErrShopCardKeyAlreadyAssigned   = infraerrors.Conflict("SHOP_CARD_KEY_ALREADY_ASSIGNED", "card key is already assigned")
	ErrShopWechatOAuthUnsupported   = infraerrors.BadRequest("SHOP_WECHAT_OAUTH_UNSUPPORTED", "shop wechat in-app OAuth payment is not supported yet")
	ErrShopFileCardOSSNotConfigured = infraerrors.BadRequest("SHOP_FILE_CARD_OSS_NOT_CONFIGURED", "shop file card OSS storage is not configured")
	ErrShopFileCardTooLarge         = infraerrors.BadRequest("SHOP_FILE_CARD_TOO_LARGE", "shop file card file must be <= 204800 bytes")
	ErrShopFileCardEmpty            = infraerrors.BadRequest("SHOP_FILE_CARD_EMPTY", "shop file card file cannot be empty")
	ErrShopFileCardUnavailable      = infraerrors.NotFound("SHOP_FILE_CARD_UNAVAILABLE", "shop file card is unavailable")
)

type ShopService struct {
	entClient            *dbent.Client
	paymentService       *PaymentService
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
	settingRepo          SettingRepository
	encryptionKey        []byte
	fileCardStoreFactory ShopFileCardObjectStoreFactory
}

type ShopServiceOption func(*ShopService)

func WithShopSettingRepository(settingRepo SettingRepository) ShopServiceOption {
	return func(s *ShopService) {
		s.settingRepo = settingRepo
	}
}

func WithShopEncryptionKey(key []byte) ShopServiceOption {
	return func(s *ShopService) {
		if len(key) == 0 {
			return
		}
		s.encryptionKey = append([]byte(nil), key...)
	}
}

func WithShopFileCardObjectStoreFactory(factory ShopFileCardObjectStoreFactory) ShopServiceOption {
	return func(s *ShopService) {
		s.fileCardStoreFactory = factory
	}
}

func NewShopService(
	entClient *dbent.Client,
	paymentService *PaymentService,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	billingCacheService *BillingCacheService,
	options ...ShopServiceOption,
) *ShopService {
	svc := &ShopService{
		entClient:            entClient,
		paymentService:       paymentService,
		authCacheInvalidator: authCacheInvalidator,
		billingCacheService:  billingCacheService,
	}
	for _, option := range options {
		if option != nil {
			option(svc)
		}
	}
	return svc
}

func (s *ShopService) ParseWeChatPaymentResumeToken(token string) (*WeChatPaymentResumeClaims, error) {
	if s == nil || s.paymentService == nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_SERVICE_NOT_CONFIGURED", "payment service is not configured")
	}
	return s.paymentService.ParseWeChatPaymentResumeToken(token)
}

type ShopCategoryDTO struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Icon        *string   `json:"icon,omitempty"`
	SortOrder   int       `json:"sort_order"`
	Enabled     bool      `json:"enabled"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ShopProductDTO struct {
	ID            int64            `json:"id"`
	CategoryID    *int64           `json:"category_id,omitempty"`
	Category      *ShopCategoryDTO `json:"category,omitempty"`
	Name          string           `json:"name"`
	CoverURL      *string          `json:"cover_url,omitempty"`
	Description   *string          `json:"description,omitempty"`
	Price         float64          `json:"price"`
	OriginalPrice *float64         `json:"original_price,omitempty"`
	Enabled       bool             `json:"enabled"`
	SortOrder     int              `json:"sort_order"`
	MinPurchase   int              `json:"min_purchase"`
	MaxPurchase   int              `json:"max_purchase"`
	AutoDelivery  bool             `json:"auto_delivery"`
	Stock         int              `json:"stock"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type ShopOrderDTO struct {
	ID                 int64                  `json:"id"`
	OrderNo            string                 `json:"order_no"`
	UserID             int64                  `json:"user_id"`
	ProductID          int64                  `json:"product_id"`
	ProductName        string                 `json:"product_name"`
	ProductCoverURL    *string                `json:"product_cover_url,omitempty"`
	ProductDescription *string                `json:"product_description,omitempty"`
	UnitPrice          float64                `json:"unit_price"`
	Quantity           int                    `json:"quantity"`
	TotalAmount        float64                `json:"total_amount"`
	PaymentMethod      string                 `json:"payment_method"`
	PaymentOrderID     *int64                 `json:"payment_order_id,omitempty"`
	Status             string                 `json:"status"`
	DeliveredCards     []string               `json:"delivered_cards"`
	DeliveredFiles     []ShopDeliveredFileDTO `json:"delivered_files"`
	PaidAt             *time.Time             `json:"paid_at,omitempty"`
	CompletedAt        *time.Time             `json:"completed_at,omitempty"`
	CancelledAt        *time.Time             `json:"cancelled_at,omitempty"`
	FailedReason       *string                `json:"failed_reason,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	Payment            *CreateOrderResponse   `json:"payment,omitempty"`
}

type ShopDeliveredFileDTO struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
	SHA256      string `json:"sha256"`
	StorageKey  string `json:"-"`
}

type ShopCardKeyDTO struct {
	ID               int64      `json:"id"`
	ProductID        int64      `json:"product_id"`
	Product          *string    `json:"product,omitempty"`
	Content          string     `json:"content"`
	CardType         string     `json:"card_type"`
	StorageProvider  *string    `json:"storage_provider,omitempty"`
	OriginalFilename *string    `json:"original_filename,omitempty"`
	ContentType      *string    `json:"content_type,omitempty"`
	ByteSize         *int64     `json:"byte_size,omitempty"`
	SHA256           *string    `json:"sha256,omitempty"`
	Status           string     `json:"status"`
	OrderID          *int64     `json:"order_id,omitempty"`
	OrderNo          *string    `json:"order_no,omitempty"`
	LockedAt         *time.Time `json:"locked_at,omitempty"`
	LockedUntil      *time.Time `json:"locked_until,omitempty"`
	SoldAt           *time.Time `json:"sold_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ShopListProductsParams struct {
	CategoryID int64
	Keyword    string
	Page       int
	PageSize   int
	Admin      bool
}

type ShopListCardKeysParams struct {
	ProductID int64
	Status    string
	Keyword   string
	Page      int
	PageSize  int
}

type ShopCreateOrderRequest struct {
	UserID          int64
	ProductID       int64
	Quantity        int
	PaymentMethod   string
	OpenID          string
	ClientIP        string
	IsMobile        bool
	IsWeChatBrowser bool
	SrcHost         string
	SrcURL          string
	ReturnURL       string
	PaymentSource   string
}

type ShopCreateCategoryRequest struct {
	Name        string  `json:"name"`
	Icon        *string `json:"icon"`
	SortOrder   int     `json:"sort_order"`
	Enabled     *bool   `json:"enabled"`
	Description *string `json:"description"`
}

type ShopUpdateCategoryRequest struct {
	Name        *string `json:"name"`
	Icon        *string `json:"icon"`
	SortOrder   *int    `json:"sort_order"`
	Enabled     *bool   `json:"enabled"`
	Description *string `json:"description"`
}

type ShopCreateProductRequest struct {
	CategoryID    *int64   `json:"category_id"`
	Name          string   `json:"name"`
	CoverURL      *string  `json:"cover_url"`
	Description   *string  `json:"description"`
	Price         float64  `json:"price"`
	OriginalPrice *float64 `json:"original_price"`
	Enabled       *bool    `json:"enabled"`
	SortOrder     int      `json:"sort_order"`
	MinPurchase   int      `json:"min_purchase"`
	MaxPurchase   int      `json:"max_purchase"`
	AutoDelivery  *bool    `json:"auto_delivery"`
}

type ShopUpdateProductRequest struct {
	CategoryID         *int64   `json:"category_id"`
	ClearCategory      bool     `json:"clear_category"`
	Name               *string  `json:"name"`
	CoverURL           *string  `json:"cover_url"`
	Description        *string  `json:"description"`
	Price              *float64 `json:"price"`
	OriginalPrice      *float64 `json:"original_price"`
	ClearOriginalPrice bool     `json:"clear_original_price"`
	Enabled            *bool    `json:"enabled"`
	SortOrder          *int     `json:"sort_order"`
	MinPurchase        *int     `json:"min_purchase"`
	MaxPurchase        *int     `json:"max_purchase"`
	AutoDelivery       *bool    `json:"auto_delivery"`
}

type ShopCreateCardKeyRequest struct {
	ProductID int64  `json:"product_id"`
	Content   string `json:"content"`
	Status    string `json:"status"`
}

type ShopImportCardKeysRequest struct {
	ProductID int64    `json:"product_id"`
	Contents  []string `json:"contents"`
}

type ShopUpdateCardKeyRequest struct {
	ProductID *int64  `json:"product_id"`
	Content   *string `json:"content"`
	Status    *string `json:"status"`
}

func (s *ShopService) ListCategories(ctx context.Context, admin bool) ([]ShopCategoryDTO, error) {
	q := s.entClient.ShopCategory.Query()
	if !admin {
		q = q.Where(shopcategory.EnabledEQ(true))
	}
	categories, err := q.Order(dbent.Asc(shopcategory.FieldSortOrder), dbent.Asc(shopcategory.FieldID)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list shop categories: %w", err)
	}
	out := make([]ShopCategoryDTO, 0, len(categories))
	for _, item := range categories {
		out = append(out, mapShopCategory(item))
	}
	return out, nil
}

func (s *ShopService) ListProducts(ctx context.Context, params ShopListProductsParams) ([]ShopProductDTO, int, error) {
	q := s.entClient.ShopProduct.Query().WithCategory()
	if !params.Admin {
		q = q.Where(
			shopproduct.EnabledEQ(true),
			shopproduct.Or(
				shopproduct.CategoryIDIsNil(),
				shopproduct.HasCategoryWith(shopcategory.EnabledEQ(true)),
			),
		)
	}
	if params.CategoryID > 0 {
		q = q.Where(shopproduct.CategoryIDEQ(params.CategoryID))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		q = q.Where(shopproduct.Or(
			shopproduct.NameContainsFold(keyword),
			shopproduct.DescriptionContainsFold(keyword),
		))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count shop products: %w", err)
	}
	pageSize, page := applyPagination(params.PageSize, params.Page)
	products, err := q.Order(dbent.Asc(shopproduct.FieldSortOrder), dbent.Asc(shopproduct.FieldID)).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list shop products: %w", err)
	}
	stock, err := s.availableStockMap(ctx, productIDs(products))
	if err != nil {
		return nil, 0, err
	}
	out := make([]ShopProductDTO, 0, len(products))
	for _, item := range products {
		out = append(out, mapShopProduct(item, stock[item.ID]))
	}
	return out, total, nil
}

func (s *ShopService) GetProduct(ctx context.Context, id int64, admin bool) (*ShopProductDTO, error) {
	q := s.entClient.ShopProduct.Query().Where(shopproduct.IDEQ(id)).WithCategory()
	if !admin {
		q = q.Where(
			shopproduct.EnabledEQ(true),
			shopproduct.Or(
				shopproduct.CategoryIDIsNil(),
				shopproduct.HasCategoryWith(shopcategory.EnabledEQ(true)),
			),
		)
	}
	product, err := q.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopProductNotFound
		}
		return nil, fmt.Errorf("get shop product: %w", err)
	}
	stock, err := s.availableStock(ctx, product.ID)
	if err != nil {
		return nil, err
	}
	dto := mapShopProduct(product, stock)
	return &dto, nil
}

func (s *ShopService) GetOrderForUser(ctx context.Context, userID, id int64) (*ShopOrderDTO, error) {
	order, err := s.entClient.ShopOrder.Query().
		Where(shoporder.IDEQ(id), shoporder.UserIDEQ(userID)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopOrderNotFound
		}
		return nil, fmt.Errorf("get shop order: %w", err)
	}
	dto := mapShopOrder(order, nil)
	if err := s.hydrateOrderDeliveredFiles(ctx, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *ShopService) GetOrderForAdmin(ctx context.Context, id int64) (*ShopOrderDTO, error) {
	order, err := s.entClient.ShopOrder.Query().
		Where(shoporder.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopOrderNotFound
		}
		return nil, fmt.Errorf("get shop order: %w", err)
	}
	dto := mapShopOrder(order, nil)
	if err := s.hydrateOrderDeliveredFiles(ctx, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *ShopService) CreateOrder(ctx context.Context, req ShopCreateOrderRequest) (*ShopOrderDTO, error) {
	req.PaymentMethod = strings.TrimSpace(req.PaymentMethod)
	if req.PaymentMethod == "" {
		return nil, ErrShopUnsupportedPayment
	}
	if req.PaymentMethod == ShopPaymentMethodBalance {
		return s.createBalanceOrder(ctx, req)
	}
	if req.IsWeChatBrowser &&
		strings.TrimSpace(req.OpenID) == "" &&
		payment.GetBasePaymentType(req.PaymentMethod) == payment.TypeWxpay &&
		s.paymentService != nil &&
		s.paymentService.usesOfficialWxpayVisibleMethod(ctx) {
		return nil, ErrShopWechatOAuthUnsupported
	}
	return s.createPlatformPaymentOrder(ctx, req)
}

func (s *ShopService) createBalanceOrder(ctx context.Context, req ShopCreateOrderRequest) (*ShopOrderDTO, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin shop balance transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	product, err := s.validateProductForPurchase(ctx, tx, req.ProductID, req.Quantity)
	if err != nil {
		return nil, err
	}
	totalAmount := normalizeShopAmount(product.Price * float64(req.Quantity))

	userQuery := tx.User.Query().Where(user.IDEQ(req.UserID))
	if shopTxSupportsRowLock(tx) {
		userQuery.ForUpdate()
	}
	u, err := userQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("lock user balance: %w", err)
	}
	if u.Balance+1e-9 < totalAmount {
		return nil, ErrShopInsufficientBalance
	}

	order, err := s.createShopOrderInTx(ctx, tx, req, product, totalAmount, ShopOrderStatusPaid)
	if err != nil {
		return nil, err
	}
	delivered, err := s.deliverOrderInTx(ctx, tx, order)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	order, err = tx.ShopOrder.UpdateOneID(order.ID).
		SetStatus(ShopOrderStatusCompleted).
		SetPaidAt(now).
		SetCompletedAt(now).
		SetDeliveredCards(delivered).
		ClearFailedReason().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("complete shop balance order: %w", err)
	}
	if _, err := tx.User.UpdateOneID(req.UserID).AddBalance(-totalAmount).Save(ctx); err != nil {
		return nil, fmt.Errorf("deduct shop balance: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit shop balance order: %w", err)
	}
	s.invalidateUserBalance(ctx, req.UserID)
	order.Unwrap()
	dto := mapShopOrder(order, nil)
	if err := s.hydrateOrderDeliveredFiles(ctx, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *ShopService) createPlatformPaymentOrder(ctx context.Context, req ShopCreateOrderRequest) (*ShopOrderDTO, error) {
	if s.paymentService == nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_SERVICE_NOT_CONFIGURED", "payment service is not configured")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin shop platform payment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	product, err := s.validateProductForPurchase(ctx, tx, req.ProductID, req.Quantity)
	if err != nil {
		return nil, err
	}
	totalAmount := normalizeShopAmount(product.Price * float64(req.Quantity))
	order, err := s.createShopOrderInTx(ctx, tx, req, product, totalAmount, ShopOrderStatusPending)
	if err != nil {
		return nil, err
	}
	paymentReq := CreateOrderRequest{
		UserID:          req.UserID,
		Amount:          totalAmount,
		PaymentType:     req.PaymentMethod,
		OpenID:          req.OpenID,
		ClientIP:        req.ClientIP,
		IsMobile:        req.IsMobile,
		IsWeChatBrowser: req.IsWeChatBrowser,
		SrcHost:         req.SrcHost,
		SrcURL:          req.SrcURL,
		ReturnURL:       req.ReturnURL,
		PaymentSource:   req.PaymentSource,
		OrderType:       payment.OrderTypeShop,
		ShopOrderID:     order.ID,
		Subject:         "Sub2API Store " + product.Name,
	}
	prep, err := s.paymentService.prepareCreateOrder(ctx, paymentReq)
	if err != nil {
		return nil, err
	}
	if prep.OAuth != nil {
		return nil, ErrShopWechatOAuthUnsupported
	}
	paymentOrder, err := s.paymentService.createOrderInExistingTx(ctx, tx, prep.Request, prep.User, prep.Plan, prep.Config, prep.OrderAmount, prep.LimitAmount, prep.FeeRate, prep.PayAmount, prep.Selection)
	if err != nil {
		return nil, err
	}
	order, err = tx.ShopOrder.UpdateOneID(order.ID).
		SetPaymentOrderID(paymentOrder.ID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("link shop payment order: %w", err)
	}
	if _, err := s.reserveCardsForOrderInTx(ctx, tx, order, paymentOrder.ExpiresAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit shop platform payment order: %w", err)
	}
	order.Unwrap()
	paymentOrder.Unwrap()

	paymentResp, err := s.paymentService.invokeProvider(ctx, paymentOrder, prep.Request, prep.Config, prep.LimitAmount, prep.PayAmountStr, prep.PayAmount, prep.Plan, prep.Selection)
	if err != nil {
		_ = s.failPendingPlatformPayment(ctx, paymentOrder.ID, err.Error(), !paymentProviderOrderPossiblyCreated(err))
		return nil, err
	}
	dto := mapShopOrder(order, paymentResp)
	if err := s.hydrateOrderDeliveredFiles(ctx, &dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *ShopService) ConfirmPaidAndDeliver(ctx context.Context, paymentOrderID int64) error {
	paymentOrder, err := s.entClient.PaymentOrder.Get(ctx, paymentOrderID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return infraerrors.NotFound("PAYMENT_ORDER_NOT_FOUND", "payment order not found")
		}
		return fmt.Errorf("get payment order: %w", err)
	}
	if paymentOrder.OrderType != payment.OrderTypeShop {
		return infraerrors.BadRequest("INVALID_PAYMENT_ORDER_TYPE", "payment order is not a shop order")
	}
	if paymentOrder.ShopOrderID == nil || *paymentOrder.ShopOrderID <= 0 {
		return infraerrors.BadRequest("INVALID_PAYMENT_ORDER", "payment order missing shop order id")
	}
	if paymentOrder.UserID <= 0 {
		return infraerrors.BadRequest("INVALID_PAYMENT_ORDER", "payment order missing user id")
	}
	if paymentOrder.Status != payment.OrderStatusPaid && paymentOrder.Status != payment.OrderStatusFailed && paymentOrder.Status != payment.OrderStatusCompleted {
		return infraerrors.BadRequest("INVALID_PAYMENT_STATUS", "payment order is not paid")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin shop fulfillment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	orderQuery := tx.ShopOrder.Query().
		Where(shoporder.IDEQ(*paymentOrder.ShopOrderID))
	if shopTxSupportsRowLock(tx) {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return ErrShopOrderNotFound
		}
		return fmt.Errorf("lock shop order: %w", err)
	}
	if order.Status == ShopOrderStatusCompleted {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit idempotent shop fulfillment: %w", err)
		}
		return nil
	}
	if (order.Status == ShopOrderStatusCancelled || order.Status == ShopOrderStatusFailed) && paymentOrder.Status == payment.OrderStatusPaid {
		if _, err := tx.ShopOrder.UpdateOneID(order.ID).
			SetStatus(ShopOrderStatusPending).
			ClearCancelledAt().
			ClearFailedReason().
			Save(ctx); err != nil {
			return fmt.Errorf("recover cancelled shop order: %w", err)
		}
		order.Status = ShopOrderStatusPending
	}
	if order.Status != ShopOrderStatusPending && order.Status != ShopOrderStatusPaid && order.Status != ShopOrderStatusFailed {
		return ErrShopInvalidOrderStatus
	}
	if order.PaymentOrderID == nil || *order.PaymentOrderID != paymentOrderID {
		return infraerrors.Conflict("SHOP_PAYMENT_LINK_MISMATCH", "shop order payment link mismatch")
	}
	if order.UserID != paymentOrder.UserID {
		return infraerrors.Conflict("SHOP_PAYMENT_USER_MISMATCH", "shop order user does not match payment order user")
	}
	if math.Abs(order.TotalAmount-paymentOrder.Amount) > shopAmountTolerance {
		_ = s.markShopFulfillmentFailedInTx(ctx, tx, order.ID, paymentOrderID, "payment amount mismatch")
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("commit shop amount mismatch failure: %w", commitErr)
		}
		return ErrShopPaymentAmountMismatch
	}

	delivered, err := s.deliverOrderInTx(ctx, tx, order)
	if err != nil {
		_ = s.markShopFulfillmentFailedInTx(ctx, tx, order.ID, paymentOrderID, err.Error())
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("commit shop fulfillment failure: %w", commitErr)
		}
		return err
	}
	paidAt := time.Now()
	if paymentOrder.PaidAt != nil {
		paidAt = *paymentOrder.PaidAt
	}
	if _, err := tx.ShopOrder.UpdateOneID(order.ID).
		SetStatus(ShopOrderStatusCompleted).
		SetPaidAt(paidAt).
		SetCompletedAt(time.Now()).
		SetDeliveredCards(delivered).
		ClearFailedReason().
		Save(ctx); err != nil {
		return fmt.Errorf("mark shop order completed: %w", err)
	}
	if _, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(paymentOrderID), paymentorder.StatusIn(payment.OrderStatusPaid, payment.OrderStatusFailed, payment.OrderStatusCompleted)).
		SetStatus(payment.OrderStatusCompleted).
		SetCompletedAt(time.Now()).
		ClearFailedAt().
		ClearFailedReason().
		Save(ctx); err != nil {
		return fmt.Errorf("mark payment order completed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shop fulfillment: %w", err)
	}
	return nil
}

func (s *ShopService) markShopFulfillmentFailedInTx(ctx context.Context, tx *dbent.Tx, shopOrderID, paymentOrderID int64, reason string) error {
	now := time.Now()
	if _, err := tx.ShopOrder.UpdateOneID(shopOrderID).
		SetStatus(ShopOrderStatusFailed).
		SetFailedReason(reason).
		ClearCancelledAt().
		Save(ctx); err != nil {
		return fmt.Errorf("mark shop order failed: %w", err)
	}
	if err := s.releaseReservedCardsInTx(ctx, tx, shopOrderID); err != nil {
		return err
	}
	if _, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(paymentOrderID), paymentorder.StatusIn(payment.OrderStatusPaid, payment.OrderStatusFailed, payment.OrderStatusCompleted)).
		SetStatus(payment.OrderStatusFailed).
		SetFailedAt(now).
		SetFailedReason(reason).
		Save(ctx); err != nil {
		return fmt.Errorf("mark shop payment failed: %w", err)
	}
	return nil
}

func (s *ShopService) failPendingPlatformPayment(ctx context.Context, paymentOrderID int64, reason string, releaseNow bool) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin shop payment failure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(paymentOrderID), paymentorder.StatusEQ(payment.OrderStatusPending)).
		SetStatus(payment.OrderStatusFailed).
		SetFailedAt(time.Now()).
		SetFailedReason(reason).
		Save(ctx); err != nil {
		return fmt.Errorf("mark shop payment failed: %w", err)
	}
	if err := s.cancelPendingPaymentInTx(ctx, tx, paymentOrderID, ShopOrderStatusFailed, reason, releaseNow); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shop payment failure transaction: %w", err)
	}
	return nil
}

func (s *ShopService) CancelPendingPayment(ctx context.Context, paymentOrderID int64, shopStatus string) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin shop payment cancel transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.cancelPendingPaymentInTx(ctx, tx, paymentOrderID, shopStatus, "payment order cancelled", false); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shop payment cancel transaction: %w", err)
	}
	return nil
}

func (s *ShopService) CancelPendingPaymentInTx(ctx context.Context, tx *dbent.Tx, paymentOrderID int64, shopStatus string) error {
	return s.cancelPendingPaymentInTx(ctx, tx, paymentOrderID, shopStatus, "payment order cancelled", false)
}

func (s *ShopService) cancelPendingPaymentInTx(ctx context.Context, tx *dbent.Tx, paymentOrderID int64, shopStatus, reason string, releaseNow bool) error {
	if shopStatus == "" {
		shopStatus = ShopOrderStatusCancelled
	}
	orderQuery := tx.ShopOrder.Query().
		Where(shoporder.PaymentOrderIDEQ(paymentOrderID))
	if shopTxSupportsRowLock(tx) {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("lock shop order for payment cancel: %w", err)
	}
	if order.Status == ShopOrderStatusCompleted {
		return nil
	}
	if order.Status != ShopOrderStatusPending && order.Status != ShopOrderStatusFailed {
		return ErrShopInvalidOrderStatus
	}
	now := time.Now()
	up := tx.ShopOrder.UpdateOneID(order.ID).
		SetStatus(shopStatus).
		SetFailedReason(reason)
	if shopStatus == ShopOrderStatusCancelled {
		up.SetCancelledAt(now)
	} else {
		up.ClearCancelledAt()
	}
	if _, err := up.Save(ctx); err != nil {
		return fmt.Errorf("cancel shop order: %w", err)
	}
	if releaseNow {
		return s.releaseReservedCardsInTx(ctx, tx, order.ID)
	}
	return nil
}

func (s *ShopService) validateProductForPurchase(ctx context.Context, tx *dbent.Tx, productID int64, quantity int) (*dbent.ShopProduct, error) {
	if quantity <= 0 {
		return nil, ErrShopInvalidQuantity
	}
	productQuery := tx.ShopProduct.Query().
		Where(shopproduct.IDEQ(productID)).
		WithCategory()
	if shopTxSupportsRowLock(tx) {
		productQuery.ForUpdate()
	}
	product, err := productQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopProductNotFound
		}
		return nil, fmt.Errorf("lock shop product: %w", err)
	}
	if !product.Enabled {
		return nil, ErrShopProductUnavailable
	}
	if product.CategoryID != nil {
		category, err := product.Edges.CategoryOrErr()
		if err != nil {
			return nil, ErrShopCategoryNotFound
		}
		if !category.Enabled {
			return nil, ErrShopProductUnavailable
		}
	}
	if !product.AutoDelivery {
		return nil, ErrShopAutoDeliveryRequired
	}
	if quantity < product.MinPurchase || quantity > product.MaxPurchase {
		return nil, ErrShopInvalidQuantity.WithMetadata(map[string]string{
			"min": fmt.Sprintf("%d", product.MinPurchase),
			"max": fmt.Sprintf("%d", product.MaxPurchase),
		})
	}
	available, err := tx.ShopCardKey.Query().
		Where(shopcardkey.ProductIDEQ(product.ID), shopcardkey.StatusEQ(ShopCardStatusAvailable)).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count shop stock: %w", err)
	}
	if available < quantity {
		return nil, ErrShopInsufficientStock.WithMetadata(map[string]string{
			"stock": fmt.Sprintf("%d", available),
		})
	}
	return product, nil
}

func (s *ShopService) createShopOrderInTx(ctx context.Context, tx *dbent.Tx, req ShopCreateOrderRequest, product *dbent.ShopProduct, totalAmount float64, status string) (*dbent.ShopOrder, error) {
	orderNo, err := s.allocateShopOrderNo(ctx, tx)
	if err != nil {
		return nil, err
	}
	return tx.ShopOrder.Create().
		SetOrderNo(orderNo).
		SetUserID(req.UserID).
		SetProductID(product.ID).
		SetProductName(product.Name).
		SetNillableProductCoverURL(product.CoverURL).
		SetNillableProductDescription(product.Description).
		SetUnitPrice(normalizeShopAmount(product.Price)).
		SetQuantity(req.Quantity).
		SetTotalAmount(totalAmount).
		SetPaymentMethod(req.PaymentMethod).
		SetStatus(status).
		SetDeliveredCards([]string{}).
		Save(ctx)
}

func (s *ShopService) deliverOrderInTx(ctx context.Context, tx *dbent.Tx, order *dbent.ShopOrder) ([]string, error) {
	if len(order.DeliveredCards) > 0 && order.Status == ShopOrderStatusCompleted {
		return order.DeliveredCards, nil
	}
	cardsQuery := tx.ShopCardKey.Query().
		Where(
			shopcardkey.ProductIDEQ(order.ProductID),
			shopcardkey.OrderIDEQ(order.ID),
			shopcardkey.StatusEQ(ShopCardStatusLocked),
		).
		Order(dbent.Asc(shopcardkey.FieldID)).
		Limit(order.Quantity)
	if shopTxSupportsRowLock(tx) {
		cardsQuery.ForUpdate()
	}
	cards, err := cardsQuery.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock reserved shop cards: %w", err)
	}
	if len(cards) > 0 && len(cards) < order.Quantity {
		return nil, ErrShopInsufficientStock.WithMetadata(map[string]string{
			"stock": fmt.Sprintf("%d", len(cards)),
		})
	}
	if len(cards) == 0 {
		return s.deliverAvailableCardsInTx(ctx, tx, order)
	}
	return s.markCardsSoldInTx(ctx, tx, order.ID, cards)
}

func (s *ShopService) deliverAvailableCardsInTx(ctx context.Context, tx *dbent.Tx, order *dbent.ShopOrder) ([]string, error) {
	cardsQuery := tx.ShopCardKey.Query().
		Where(
			shopcardkey.ProductIDEQ(order.ProductID),
			shopcardkey.StatusEQ(ShopCardStatusAvailable),
			shopcardkey.OrderIDIsNil(),
		).
		Order(dbent.Asc(shopcardkey.FieldID)).
		Limit(order.Quantity)
	if shopTxSupportsRowLock(tx) {
		cardsQuery.ForUpdate(entsql.WithLockAction(entsql.SkipLocked))
	}
	cards, err := cardsQuery.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock available shop cards: %w", err)
	}
	if len(cards) < order.Quantity {
		return nil, ErrShopInsufficientStock.WithMetadata(map[string]string{
			"stock": fmt.Sprintf("%d", len(cards)),
		})
	}
	return s.markCardsSoldInTx(ctx, tx, order.ID, cards)
}

func (s *ShopService) markCardsSoldInTx(ctx context.Context, tx *dbent.Tx, orderID int64, cards []*dbent.ShopCardKey) ([]string, error) {
	now := time.Now()
	delivered := make([]string, 0, len(cards))
	cardTypes, err := s.cardTypesInTx(ctx, tx, shopCardIDs(cards))
	if err != nil {
		return nil, err
	}
	for _, card := range cards {
		if cardTypes[card.ID] != ShopCardTypeFile {
			delivered = append(delivered, card.Content)
		}
		predicates := []predicate.ShopCardKey{
			shopcardkey.IDEQ(card.ID),
			shopcardkey.ProductIDEQ(card.ProductID),
		}
		switch card.Status {
		case ShopCardStatusLocked:
			predicates = append(predicates, shopcardkey.StatusEQ(ShopCardStatusLocked), shopcardkey.OrderIDEQ(orderID))
		case ShopCardStatusAvailable:
			predicates = append(predicates, shopcardkey.StatusEQ(ShopCardStatusAvailable), shopcardkey.OrderIDIsNil())
		default:
			return nil, ErrShopInsufficientStock.WithMetadata(map[string]string{
				"stock": "0",
			})
		}
		updated, err := tx.ShopCardKey.Update().
			Where(predicates...).
			SetStatus(ShopCardStatusSold).
			SetOrderID(orderID).
			SetSoldAt(now).
			ClearLockedAt().
			ClearLockedUntil().
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("mark shop card sold: %w", err)
		}
		if updated != 1 {
			return nil, ErrShopInsufficientStock.WithMetadata(map[string]string{
				"stock": "0",
			})
		}
	}
	return delivered, nil
}

func (s *ShopService) reserveCardsForOrderInTx(ctx context.Context, tx *dbent.Tx, order *dbent.ShopOrder, lockedUntil time.Time) ([]string, error) {
	cardsQuery := tx.ShopCardKey.Query().
		Where(
			shopcardkey.ProductIDEQ(order.ProductID),
			shopcardkey.StatusEQ(ShopCardStatusAvailable),
			shopcardkey.OrderIDIsNil(),
		).
		Order(dbent.Asc(shopcardkey.FieldID)).
		Limit(order.Quantity)
	if shopTxSupportsRowLock(tx) {
		cardsQuery.ForUpdate(entsql.WithLockAction(entsql.SkipLocked))
	}
	cards, err := cardsQuery.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock shop cards for reservation: %w", err)
	}
	if len(cards) < order.Quantity {
		return nil, ErrShopInsufficientStock.WithMetadata(map[string]string{
			"stock": fmt.Sprintf("%d", len(cards)),
		})
	}
	now := time.Now()
	contents := make([]string, 0, len(cards))
	cardTypes, err := s.cardTypesInTx(ctx, tx, shopCardIDs(cards))
	if err != nil {
		return nil, err
	}
	for _, card := range cards {
		if cardTypes[card.ID] != ShopCardTypeFile {
			contents = append(contents, card.Content)
		}
		if _, err := tx.ShopCardKey.UpdateOneID(card.ID).
			SetStatus(ShopCardStatusLocked).
			SetOrderID(order.ID).
			SetLockedAt(now).
			SetLockedUntil(lockedUntil).
			ClearSoldAt().
			Save(ctx); err != nil {
			return nil, fmt.Errorf("reserve shop card: %w", err)
		}
	}
	return contents, nil
}

func (s *ShopService) releaseReservedCardsInTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	if _, err := tx.ShopCardKey.Update().
		Where(shopcardkey.OrderIDEQ(orderID), shopcardkey.StatusEQ(ShopCardStatusLocked)).
		SetStatus(ShopCardStatusAvailable).
		ClearOrderID().
		ClearLockedAt().
		ClearLockedUntil().
		ClearSoldAt().
		Save(ctx); err != nil {
		return fmt.Errorf("release reserved shop cards: %w", err)
	}
	return nil
}

func (s *ShopService) allocateShopOrderNo(ctx context.Context, tx *dbent.Tx) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		candidate := "SHOP" + time.Now().Format("20060102150405") + strings.ToUpper(generateRandomString(6))
		exists, err := tx.ShopOrder.Query().Where(shoporder.OrderNoEQ(candidate)).Exist(ctx)
		if err != nil {
			return "", fmt.Errorf("check shop order no: %w", err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("generate shop order no: exhausted attempts")
}

func (s *ShopService) availableStock(ctx context.Context, productID int64) (int, error) {
	if err := s.releaseExpiredReservations(ctx); err != nil {
		return 0, err
	}
	count, err := s.entClient.ShopCardKey.Query().
		Where(shopcardkey.ProductIDEQ(productID), shopcardkey.StatusEQ(ShopCardStatusAvailable)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count shop stock: %w", err)
	}
	return count, nil
}

func (s *ShopService) availableStockMap(ctx context.Context, ids []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if err := s.releaseExpiredReservations(ctx); err != nil {
		return nil, err
	}
	type row struct {
		ProductID int64 `json:"product_id"`
		Count     int   `json:"count"`
	}
	var rows []row
	err := s.entClient.ShopCardKey.Query().
		Where(shopcardkey.ProductIDIn(ids...), shopcardkey.StatusEQ(ShopCardStatusAvailable)).
		GroupBy(shopcardkey.FieldProductID).
		Aggregate(dbent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("count shop stock map: %w", err)
	}
	for _, r := range rows {
		out[r.ProductID] = r.Count
	}
	return out, nil
}

func (s *ShopService) releaseExpiredReservations(ctx context.Context) error {
	now := time.Now()
	return s.ReleaseStalePaymentReservations(ctx, now.Add(-paymentGraceMinutes*time.Minute))
}

func (s *ShopService) ReleaseStalePaymentReservations(ctx context.Context, cutoff time.Time) error {
	staleOrders, err := s.entClient.ShopOrder.Query().
		Where(
			shoporder.StatusIn(ShopOrderStatusCancelled, ShopOrderStatusFailed),
			shoporder.HasCardKeysWith(
				shopcardkey.StatusEQ(ShopCardStatusLocked),
				shopcardkey.LockedUntilLTE(cutoff),
			),
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query stale shop reservations: %w", err)
	}
	for _, order := range staleOrders {
		tx, err := s.entClient.Tx(ctx)
		if err != nil {
			return fmt.Errorf("begin stale shop reservation release transaction: %w", err)
		}
		if err := s.releaseStalePaymentReservationInTx(ctx, tx, order.ID, cutoff); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("commit stale shop reservation release transaction: %w", err)
		}
	}
	return nil
}

func (s *ShopService) releaseStalePaymentReservationInTx(ctx context.Context, tx *dbent.Tx, orderID int64, cutoff time.Time) error {
	orderQuery := tx.ShopOrder.Query().
		Where(shoporder.IDEQ(orderID))
	if shopTxSupportsRowLock(tx) {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("lock stale shop order: %w", err)
	}
	if order.Status != ShopOrderStatusCancelled && order.Status != ShopOrderStatusFailed {
		return nil
	}
	if _, err := tx.ShopCardKey.Update().
		Where(
			shopcardkey.OrderIDEQ(order.ID),
			shopcardkey.StatusEQ(ShopCardStatusLocked),
			shopcardkey.LockedUntilLTE(cutoff),
		).
		SetStatus(ShopCardStatusAvailable).
		ClearOrderID().
		ClearLockedAt().
		ClearLockedUntil().
		ClearSoldAt().
		Save(ctx); err != nil {
		return fmt.Errorf("release stale shop reservations: %w", err)
	}
	return nil
}

func productIDs(products []*dbent.ShopProduct) []int64 {
	ids := make([]int64, 0, len(products))
	for _, item := range products {
		ids = append(ids, item.ID)
	}
	return ids
}

func (s *ShopService) invalidateUserBalance(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateUserBalance(ctx, userID)
	}
}

func shopTxSupportsRowLock(tx *dbent.Tx) bool {
	return tx != nil && tx.Driver().Dialect() == dialect.Postgres
}

func (s *ShopService) AdminListCategories(ctx context.Context) ([]ShopCategoryDTO, error) {
	return s.ListCategories(ctx, true)
}

func (s *ShopService) AdminCreateCategory(ctx context.Context, req ShopCreateCategoryRequest) (*ShopCategoryDTO, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrShopInvalidInput
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item, err := s.entClient.ShopCategory.Create().
		SetName(name).
		SetNillableIcon(normalizeOptionalString(req.Icon)).
		SetSortOrder(req.SortOrder).
		SetEnabled(enabled).
		SetNillableDescription(normalizeOptionalString(req.Description)).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create shop category: %w", err)
	}
	dto := mapShopCategory(item)
	return &dto, nil
}

func (s *ShopService) AdminUpdateCategory(ctx context.Context, id int64, req ShopUpdateCategoryRequest) (*ShopCategoryDTO, error) {
	if _, err := s.entClient.ShopCategory.Get(ctx, id); err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopCategoryNotFound
		}
		return nil, fmt.Errorf("get shop category: %w", err)
	}
	up := s.entClient.ShopCategory.UpdateOneID(id)
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrShopInvalidInput
		}
		up.SetName(name)
	}
	if req.Icon != nil {
		if v := normalizeOptionalString(req.Icon); v != nil {
			up.SetIcon(*v)
		} else {
			up.ClearIcon()
		}
	}
	if req.Description != nil {
		if v := normalizeOptionalString(req.Description); v != nil {
			up.SetDescription(*v)
		} else {
			up.ClearDescription()
		}
	}
	if req.SortOrder != nil {
		up.SetSortOrder(*req.SortOrder)
	}
	if req.Enabled != nil {
		up.SetEnabled(*req.Enabled)
	}
	item, err := up.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update shop category: %w", err)
	}
	dto := mapShopCategory(item)
	return &dto, nil
}

func (s *ShopService) AdminDeleteCategory(ctx context.Context, id int64) error {
	_, err := s.entClient.ShopCategory.UpdateOneID(id).SetEnabled(false).Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return ErrShopCategoryNotFound
		}
		return fmt.Errorf("disable shop category: %w", err)
	}
	return nil
}

func (s *ShopService) AdminListProducts(ctx context.Context, params ShopListProductsParams) ([]ShopProductDTO, int, error) {
	params.Admin = true
	return s.ListProducts(ctx, params)
}

func (s *ShopService) AdminCreateProduct(ctx context.Context, req ShopCreateProductRequest) (*ShopProductDTO, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrShopInvalidInput
	}
	minPurchase, maxPurchase, err := normalizePurchaseRange(req.MinPurchase, req.MaxPurchase)
	if err != nil {
		return nil, err
	}
	if req.Price < 0 || math.IsNaN(req.Price) || math.IsInf(req.Price, 0) {
		return nil, ErrShopInvalidInput
	}
	if err := validateOptionalShopPrice(req.OriginalPrice); err != nil {
		return nil, err
	}
	if err := s.ensureCategoryExists(ctx, req.CategoryID); err != nil {
		return nil, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	autoDelivery := true
	if req.AutoDelivery != nil {
		autoDelivery = *req.AutoDelivery
	}
	create := s.entClient.ShopProduct.Create().
		SetName(name).
		SetNillableCategoryID(req.CategoryID).
		SetNillableCoverURL(normalizeOptionalString(req.CoverURL)).
		SetNillableDescription(normalizeOptionalString(req.Description)).
		SetPrice(normalizeShopAmount(req.Price)).
		SetNillableOriginalPrice(normalizeOptionalShopPrice(req.OriginalPrice)).
		SetEnabled(enabled).
		SetSortOrder(req.SortOrder).
		SetMinPurchase(minPurchase).
		SetMaxPurchase(maxPurchase).
		SetAutoDelivery(autoDelivery)
	item, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create shop product: %w", err)
	}
	dto := mapShopProduct(item, 0)
	return &dto, nil
}

func (s *ShopService) AdminUpdateProduct(ctx context.Context, id int64, req ShopUpdateProductRequest) (*ShopProductDTO, error) {
	current, err := s.entClient.ShopProduct.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopProductNotFound
		}
		return nil, fmt.Errorf("get shop product: %w", err)
	}
	minPurchase := current.MinPurchase
	maxPurchase := current.MaxPurchase
	if req.MinPurchase != nil {
		minPurchase = *req.MinPurchase
	}
	if req.MaxPurchase != nil {
		maxPurchase = *req.MaxPurchase
	}
	if _, _, err := normalizePurchaseRange(minPurchase, maxPurchase); err != nil {
		return nil, err
	}
	if req.Price != nil && (*req.Price < 0 || math.IsNaN(*req.Price) || math.IsInf(*req.Price, 0)) {
		return nil, ErrShopInvalidInput
	}
	if err := validateOptionalShopPrice(req.OriginalPrice); err != nil {
		return nil, err
	}
	if !req.ClearCategory {
		if err := s.ensureCategoryExists(ctx, req.CategoryID); err != nil {
			return nil, err
		}
	}
	up := s.entClient.ShopProduct.UpdateOneID(id)
	if req.ClearCategory {
		up.ClearCategoryID()
	} else if req.CategoryID != nil {
		up.SetCategoryID(*req.CategoryID)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrShopInvalidInput
		}
		up.SetName(name)
	}
	if req.CoverURL != nil {
		if v := normalizeOptionalString(req.CoverURL); v != nil {
			up.SetCoverURL(*v)
		} else {
			up.ClearCoverURL()
		}
	}
	if req.Description != nil {
		if v := normalizeOptionalString(req.Description); v != nil {
			up.SetDescription(*v)
		} else {
			up.ClearDescription()
		}
	}
	if req.Price != nil {
		up.SetPrice(normalizeShopAmount(*req.Price))
	}
	if req.ClearOriginalPrice {
		up.ClearOriginalPrice()
	} else if req.OriginalPrice != nil {
		up.SetOriginalPrice(normalizeShopAmount(*req.OriginalPrice))
	}
	if req.Enabled != nil {
		up.SetEnabled(*req.Enabled)
	}
	if req.SortOrder != nil {
		up.SetSortOrder(*req.SortOrder)
	}
	if req.MinPurchase != nil {
		up.SetMinPurchase(*req.MinPurchase)
	}
	if req.MaxPurchase != nil {
		up.SetMaxPurchase(*req.MaxPurchase)
	}
	if req.AutoDelivery != nil {
		up.SetAutoDelivery(*req.AutoDelivery)
	}
	item, err := up.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update shop product: %w", err)
	}
	stock, err := s.availableStock(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	dto := mapShopProduct(item, stock)
	return &dto, nil
}

func (s *ShopService) AdminDeleteProduct(ctx context.Context, id int64) error {
	_, err := s.entClient.ShopProduct.UpdateOneID(id).SetEnabled(false).Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return ErrShopProductNotFound
		}
		return fmt.Errorf("disable shop product: %w", err)
	}
	return nil
}

func (s *ShopService) AdminListCardKeys(ctx context.Context, params ShopListCardKeysParams) ([]ShopCardKeyDTO, int, error) {
	q := s.entClient.ShopCardKey.Query().WithProduct().WithOrder()
	if params.ProductID > 0 {
		q = q.Where(shopcardkey.ProductIDEQ(params.ProductID))
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		q = q.Where(shopcardkey.StatusEQ(status))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		q = q.Where(shopcardkey.ContentContainsFold(keyword))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count shop card keys: %w", err)
	}
	pageSize, page := applyPagination(params.PageSize, params.Page)
	items, err := q.Order(dbent.Desc(shopcardkey.FieldID)).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list shop card keys: %w", err)
	}
	out := make([]ShopCardKeyDTO, 0, len(items))
	for _, item := range items {
		out = append(out, mapShopCardKey(item))
	}
	out, err = s.decorateCardKeyDTOsWithFileMeta(ctx, out)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *ShopService) AdminCreateCardKey(ctx context.Context, req ShopCreateCardKeyRequest) (*ShopCardKeyDTO, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, ErrShopInvalidInput
	}
	status := normalizeCardKeyStatus(req.Status)
	if status == "" || status == ShopCardStatusSold || status == ShopCardStatusLocked {
		return nil, ErrShopInvalidInput
	}
	if err := s.ensureProductExists(ctx, req.ProductID); err != nil {
		return nil, err
	}
	item, err := s.entClient.ShopCardKey.Create().
		SetProductID(req.ProductID).
		SetContent(content).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create shop card key: %w", err)
	}
	dto := mapShopCardKey(item)
	dto.CardType = ShopCardTypeText
	return &dto, nil
}

func (s *ShopService) AdminImportCardKeys(ctx context.Context, req ShopImportCardKeysRequest) ([]ShopCardKeyDTO, error) {
	if err := s.ensureProductExists(ctx, req.ProductID); err != nil {
		return nil, err
	}
	contents := make([]string, 0, len(req.Contents))
	seen := map[string]struct{}{}
	for _, raw := range req.Contents {
		content := strings.TrimSpace(raw)
		if content == "" {
			continue
		}
		if _, ok := seen[content]; ok {
			continue
		}
		seen[content] = struct{}{}
		contents = append(contents, content)
	}
	if len(contents) == 0 {
		return nil, ErrShopInvalidInput
	}
	bulk := make([]*dbent.ShopCardKeyCreate, 0, len(contents))
	for _, content := range contents {
		bulk = append(bulk, s.entClient.ShopCardKey.Create().
			SetProductID(req.ProductID).
			SetContent(content).
			SetStatus(ShopCardStatusAvailable))
	}
	items, err := s.entClient.ShopCardKey.CreateBulk(bulk...).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("import shop card keys: %w", err)
	}
	out := make([]ShopCardKeyDTO, 0, len(items))
	for _, item := range items {
		dto := mapShopCardKey(item)
		dto.CardType = ShopCardTypeText
		out = append(out, dto)
	}
	return out, nil
}

func (s *ShopService) AdminUpdateCardKey(ctx context.Context, id int64, req ShopUpdateCardKeyRequest) (*ShopCardKeyDTO, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin shop card update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	currentQuery := tx.ShopCardKey.Query().
		Where(shopcardkey.IDEQ(id)).
		WithProduct()
	if shopTxSupportsRowLock(tx) {
		currentQuery.ForUpdate()
	}
	current, err := currentQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrShopCardKeyNotFound
		}
		return nil, fmt.Errorf("lock shop card key: %w", err)
	}
	if current.Status == ShopCardStatusSold || current.Status == ShopCardStatusLocked || current.OrderID != nil {
		return nil, ErrShopCardKeyAlreadyAssigned
	}
	if req.ProductID != nil {
		if err := s.ensureProductExistsInTx(ctx, tx, *req.ProductID); err != nil {
			return nil, err
		}
	}
	up := tx.ShopCardKey.UpdateOneID(id)
	if req.ProductID != nil {
		up.SetProductID(*req.ProductID)
	}
	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			return nil, ErrShopInvalidInput
		}
		up.SetContent(content)
	}
	if req.Status != nil {
		status := normalizeCardKeyStatus(*req.Status)
		if status == "" || status == ShopCardStatusSold || status == ShopCardStatusLocked {
			return nil, ErrShopInvalidInput
		}
		up.SetStatus(status)
	}
	item, err := up.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update shop card key: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit shop card update transaction: %w", err)
	}
	item.Unwrap()
	dto := mapShopCardKey(item)
	if decorated, err := s.decorateCardKeyDTOsWithFileMeta(ctx, []ShopCardKeyDTO{dto}); err == nil && len(decorated) == 1 {
		dto = decorated[0]
	} else if err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *ShopService) AdminDeleteCardKey(ctx context.Context, id int64) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin shop card delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	currentQuery := tx.ShopCardKey.Query().
		Where(shopcardkey.IDEQ(id))
	if shopTxSupportsRowLock(tx) {
		currentQuery.ForUpdate()
	}
	current, err := currentQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return ErrShopCardKeyNotFound
		}
		return fmt.Errorf("lock shop card key: %w", err)
	}
	if current.Status == ShopCardStatusSold || current.Status == ShopCardStatusLocked || current.OrderID != nil {
		return ErrShopCardKeyAlreadyAssigned
	}
	var meta map[int64]shopFileCardMeta
	if queryer, ok := tx.Driver().(shopSQLQueryer); ok {
		meta, err = fileCardMetaByIDsWithQueryer(ctx, queryer, []int64{id})
		if err != nil {
			if !isShopUndefinedColumnError(err) {
				return err
			}
			meta = nil
		}
	}
	var fileStorageKey string
	if item, ok := meta[id]; ok && item.CardType == ShopCardTypeFile && item.StorageKey.Valid {
		fileStorageKey = strings.TrimSpace(item.StorageKey.String)
	}
	var fileStore ShopFileCardObjectStore
	if fileStorageKey != "" {
		fileStore, err = s.fileCardStoreFromSettings(ctx)
		if err != nil {
			return err
		}
	}
	if err := tx.ShopCardKey.DeleteOneID(id).Exec(ctx); err != nil {
		return fmt.Errorf("delete shop card key: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit shop card delete transaction: %w", err)
	}
	if fileStorageKey != "" {
		if err := fileStore.Delete(ctx, fileStorageKey); err != nil {
			return fmt.Errorf("delete shop file card object: %w", err)
		}
	}
	return nil
}

func (s *ShopService) ensureCategoryExists(ctx context.Context, id *int64) error {
	if id == nil || *id <= 0 {
		return nil
	}
	exists, err := s.entClient.ShopCategory.Query().Where(shopcategory.IDEQ(*id)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check shop category: %w", err)
	}
	if !exists {
		return ErrShopCategoryNotFound
	}
	return nil
}

func (s *ShopService) ensureProductExists(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrShopProductNotFound
	}
	exists, err := s.entClient.ShopProduct.Query().Where(shopproduct.IDEQ(id)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check shop product: %w", err)
	}
	if !exists {
		return ErrShopProductNotFound
	}
	return nil
}

func (s *ShopService) ensureProductExistsInTx(ctx context.Context, tx *dbent.Tx, id int64) error {
	if id <= 0 {
		return ErrShopProductNotFound
	}
	exists, err := tx.ShopProduct.Query().Where(shopproduct.IDEQ(id)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check shop product: %w", err)
	}
	if !exists {
		return ErrShopProductNotFound
	}
	return nil
}

func normalizeShopAmount(amount float64) float64 {
	return math.Round(amount*100) / 100
}

func normalizeOptionalShopPrice(price *float64) *float64 {
	if price == nil {
		return nil
	}
	normalized := normalizeShopAmount(*price)
	return &normalized
}

func validateOptionalShopPrice(price *float64) error {
	if price == nil {
		return nil
	}
	if *price < 0 || math.IsNaN(*price) || math.IsInf(*price, 0) {
		return ErrShopInvalidInput
	}
	return nil
}

func normalizePurchaseRange(minPurchase, maxPurchase int) (int, int, error) {
	if minPurchase <= 0 {
		minPurchase = 1
	}
	if maxPurchase <= 0 {
		maxPurchase = minPurchase
	}
	if maxPurchase < minPurchase {
		return 0, 0, ErrShopInvalidQuantity
	}
	return minPurchase, maxPurchase, nil
}

func normalizeOptionalString(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeCardKeyStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return ShopCardStatusAvailable
	}
	switch status {
	case ShopCardStatusAvailable, ShopCardStatusDisabled:
		return status
	default:
		return ""
	}
}

func mapShopCategory(item *dbent.ShopCategory) ShopCategoryDTO {
	return ShopCategoryDTO{
		ID:          item.ID,
		Name:        item.Name,
		Icon:        item.Icon,
		SortOrder:   item.SortOrder,
		Enabled:     item.Enabled,
		Description: item.Description,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func mapShopProduct(item *dbent.ShopProduct, stock int) ShopProductDTO {
	dto := ShopProductDTO{
		ID:            item.ID,
		CategoryID:    item.CategoryID,
		Name:          item.Name,
		CoverURL:      item.CoverURL,
		Description:   item.Description,
		Price:         item.Price,
		OriginalPrice: item.OriginalPrice,
		Enabled:       item.Enabled,
		SortOrder:     item.SortOrder,
		MinPurchase:   item.MinPurchase,
		MaxPurchase:   item.MaxPurchase,
		AutoDelivery:  item.AutoDelivery,
		Stock:         stock,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
	if category, err := item.Edges.CategoryOrErr(); err == nil && category != nil {
		c := mapShopCategory(category)
		dto.Category = &c
	}
	return dto
}

func mapShopOrder(item *dbent.ShopOrder, paymentResp *CreateOrderResponse) ShopOrderDTO {
	delivered := item.DeliveredCards
	if delivered == nil {
		delivered = []string{}
	}
	return ShopOrderDTO{
		ID:                 item.ID,
		OrderNo:            item.OrderNo,
		UserID:             item.UserID,
		ProductID:          item.ProductID,
		ProductName:        item.ProductName,
		ProductCoverURL:    item.ProductCoverURL,
		ProductDescription: item.ProductDescription,
		UnitPrice:          item.UnitPrice,
		Quantity:           item.Quantity,
		TotalAmount:        item.TotalAmount,
		PaymentMethod:      item.PaymentMethod,
		PaymentOrderID:     item.PaymentOrderID,
		Status:             item.Status,
		DeliveredCards:     delivered,
		DeliveredFiles:     []ShopDeliveredFileDTO{},
		PaidAt:             item.PaidAt,
		CompletedAt:        item.CompletedAt,
		CancelledAt:        item.CancelledAt,
		FailedReason:       item.FailedReason,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
		Payment:            paymentResp,
	}
}

func mapShopCardKey(item *dbent.ShopCardKey) ShopCardKeyDTO {
	var productName *string
	if product, err := item.Edges.ProductOrErr(); err == nil && product != nil {
		productName = &product.Name
	}
	var orderNo *string
	if order, err := item.Edges.OrderOrErr(); err == nil && order != nil {
		orderNo = &order.OrderNo
	}
	return ShopCardKeyDTO{
		ID:          item.ID,
		ProductID:   item.ProductID,
		Product:     productName,
		Content:     item.Content,
		CardType:    ShopCardTypeText,
		Status:      item.Status,
		OrderID:     item.OrderID,
		OrderNo:     orderNo,
		LockedAt:    item.LockedAt,
		LockedUntil: item.LockedUntil,
		SoldAt:      item.SoldAt,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}
