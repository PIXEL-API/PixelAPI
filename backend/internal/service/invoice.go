package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	InvoiceTypePersonalNormal    = "personal_normal"
	InvoiceTypeEnterpriseNormal  = "enterprise_normal"
	InvoiceTypeEnterpriseSpecial = "enterprise_special"

	InvoiceBuyerTypePersonal   = "personal"
	InvoiceBuyerTypeEnterprise = "enterprise"

	InvoiceSourceTypePaymentOrder = "payment_order"
	InvoiceSourceTypeRedeemCode   = "redeem_code"

	InvoiceStatusPending   = "pending"
	InvoiceStatusIssued    = "issued"
	InvoiceStatusRejected  = "rejected"
	InvoiceStatusCancelled = "cancelled"
)

var (
	ErrInvoiceManagementDisabled = infraerrors.Forbidden("INVOICE_MANAGEMENT_DISABLED", "invoice management is disabled")
	ErrInvoiceProfileNotFound    = infraerrors.NotFound("INVOICE_PROFILE_NOT_FOUND", "invoice profile not found")
	ErrInvoiceRequestNotFound    = infraerrors.NotFound("INVOICE_REQUEST_NOT_FOUND", "invoice request not found")
	ErrInvoiceTypeInvalid        = infraerrors.BadRequest("INVOICE_TYPE_INVALID", "invalid invoice type")
	ErrInvoiceFieldRequired      = infraerrors.BadRequest("INVOICE_FIELD_REQUIRED", "required invoice field is missing")
	ErrInvoiceEmailInvalid       = infraerrors.BadRequest("INVOICE_EMAIL_INVALID", "recipient email is invalid")
	ErrInvoiceSourceRequired     = infraerrors.BadRequest("INVOICE_SOURCE_REQUIRED", "at least one invoice source is required")
	ErrInvoiceSourceInvalid      = infraerrors.BadRequest("INVOICE_SOURCE_INVALID", "invalid invoice source")
	ErrInvoiceSourceUnavailable  = infraerrors.Conflict("INVOICE_SOURCE_UNAVAILABLE", "invoice source is unavailable or already occupied")
	ErrInvoiceDefaultConflict    = infraerrors.Conflict("INVOICE_DEFAULT_CONFLICT", "only one default invoice profile is allowed")
	ErrInvoiceAmountInvalid      = infraerrors.BadRequest("INVOICE_AMOUNT_INVALID", "invoice amount must be greater than zero")
	ErrInvoiceCannotCancel       = infraerrors.Conflict("INVOICE_CANNOT_CANCEL", "only pending invoice requests can be cancelled")
	ErrInvoiceCannotIssue        = infraerrors.Conflict("INVOICE_CANNOT_ISSUE", "only pending invoice requests can be issued")
	ErrInvoiceCannotReject       = infraerrors.Conflict("INVOICE_CANNOT_REJECT", "only pending invoice requests can be rejected")
)

type InvoiceProfile struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"user_id"`
	InvoiceType       string    `json:"invoice_type"`
	BuyerType         string    `json:"buyer_type"`
	TitleName         string    `json:"title_name"`
	TaxID             string    `json:"tax_id"`
	RegisteredAddress string    `json:"registered_address"`
	RegisteredPhone   string    `json:"registered_phone"`
	BankName          string    `json:"bank_name"`
	BankAccount       string    `json:"bank_account"`
	RecipientEmail    string    `json:"recipient_email"`
	RecipientPhone    string    `json:"recipient_phone"`
	IsDefault         bool      `json:"is_default"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type InvoiceProfileInput struct {
	InvoiceType       string `json:"invoice_type"`
	TitleName         string `json:"title_name"`
	TaxID             string `json:"tax_id"`
	RegisteredAddress string `json:"registered_address"`
	RegisteredPhone   string `json:"registered_phone"`
	BankName          string `json:"bank_name"`
	BankAccount       string `json:"bank_account"`
	RecipientEmail    string `json:"recipient_email"`
	RecipientPhone    string `json:"recipient_phone"`
	IsDefault         bool   `json:"is_default"`
}

type InvoiceSourceRef struct {
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
}

type InvoiceEligibleSource struct {
	SourceType        string    `json:"source_type"`
	SourceID          int64     `json:"source_id"`
	SourceNo          string    `json:"source_no"`
	SourceLabel       string    `json:"source_label"`
	ItemType          string    `json:"item_type"`
	EntitlementAmount float64   `json:"entitlement_amount"`
	InvoiceAmount     float64   `json:"invoice_amount"`
	OccurredAt        time.Time `json:"occurred_at"`
	Status            string    `json:"status"`
}

type InvoiceRequestItem struct {
	ID                int64     `json:"id"`
	InvoiceRequestID  int64     `json:"invoice_request_id"`
	SourceType        string    `json:"source_type"`
	SourceID          int64     `json:"source_id"`
	SourceNo          string    `json:"source_no"`
	SourceLabel       string    `json:"source_label"`
	ItemType          string    `json:"item_type"`
	EntitlementAmount float64   `json:"entitlement_amount"`
	InvoiceAmount     float64   `json:"invoice_amount"`
	OccurredAt        time.Time `json:"occurred_at"`
	Active            bool      `json:"active"`
	CreatedAt         time.Time `json:"created_at"`
}

type InvoiceRequest struct {
	ID                int64                `json:"id"`
	RequestNo         string               `json:"request_no"`
	UserID            int64                `json:"user_id"`
	UserEmail         string               `json:"user_email"`
	InvoiceType       string               `json:"invoice_type"`
	BuyerType         string               `json:"buyer_type"`
	TitleName         string               `json:"title_name"`
	TaxID             string               `json:"tax_id"`
	RegisteredAddress string               `json:"registered_address"`
	RegisteredPhone   string               `json:"registered_phone"`
	BankName          string               `json:"bank_name"`
	BankAccount       string               `json:"bank_account"`
	RecipientEmail    string               `json:"recipient_email"`
	RecipientPhone    string               `json:"recipient_phone"`
	Amount            float64              `json:"amount"`
	Currency          string               `json:"currency"`
	Status            string               `json:"status"`
	InvoiceNumber     string               `json:"invoice_number"`
	InvoiceCode       string               `json:"invoice_code"`
	InvoiceFileURL    string               `json:"invoice_file_url"`
	InvoiceFileName   string               `json:"invoice_file_name"`
	IssuedAt          *time.Time           `json:"issued_at,omitempty"`
	RejectedReason    *string              `json:"rejected_reason,omitempty"`
	AdminNote         *string              `json:"admin_note,omitempty"`
	ProcessedByUserID *int64               `json:"processed_by_user_id,omitempty"`
	SubmittedAt       time.Time            `json:"submitted_at"`
	ProcessedAt       *time.Time           `json:"processed_at,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	Items             []InvoiceRequestItem `json:"items,omitempty"`
}

type InvoiceRequestInput struct {
	InvoiceType       string             `json:"invoice_type"`
	TitleName         string             `json:"title_name"`
	TaxID             string             `json:"tax_id"`
	RegisteredAddress string             `json:"registered_address"`
	RegisteredPhone   string             `json:"registered_phone"`
	BankName          string             `json:"bank_name"`
	BankAccount       string             `json:"bank_account"`
	RecipientEmail    string             `json:"recipient_email"`
	RecipientPhone    string             `json:"recipient_phone"`
	SourceRefs        []InvoiceSourceRef `json:"source_refs"`
}

type InvoiceIssueInput struct {
	InvoiceNumber   string `json:"invoice_number"`
	InvoiceCode     string `json:"invoice_code"`
	InvoiceFileURL  string `json:"invoice_file_url"`
	InvoiceFileName string `json:"invoice_file_name"`
	AdminNote       string `json:"admin_note"`
}

type InvoiceRequestListParams struct {
	Page     int
	PageSize int
	UserID   int64
	Status   string
	Keyword  string
}

type InvoiceRepository interface {
	ListProfiles(ctx context.Context, userID int64) ([]InvoiceProfile, error)
	CreateProfile(ctx context.Context, userID int64, input InvoiceProfileInput) (*InvoiceProfile, error)
	UpdateProfile(ctx context.Context, userID, id int64, input InvoiceProfileInput) (*InvoiceProfile, error)
	DeleteProfile(ctx context.Context, userID, id int64) error
	SetDefaultProfile(ctx context.Context, userID, id int64) (*InvoiceProfile, error)
	ListEligibleSources(ctx context.Context, userID int64, page, pageSize int) ([]InvoiceEligibleSource, int64, error)
	CreateRequest(ctx context.Context, userID int64, input InvoiceRequestInput, requestNo string) (*InvoiceRequest, error)
	ListRequestsByUser(ctx context.Context, userID int64, params InvoiceRequestListParams) ([]InvoiceRequest, int64, error)
	GetRequestByUser(ctx context.Context, userID, id int64) (*InvoiceRequest, error)
	CancelRequest(ctx context.Context, userID, id int64) (*InvoiceRequest, error)
	ListRequestsAdmin(ctx context.Context, params InvoiceRequestListParams) ([]InvoiceRequest, int64, error)
	GetRequestByID(ctx context.Context, id int64) (*InvoiceRequest, error)
	IssueRequest(ctx context.Context, id, adminUserID int64, input InvoiceIssueInput) (*InvoiceRequest, error)
	RejectRequest(ctx context.Context, id, adminUserID int64, reason, adminNote string) (*InvoiceRequest, error)
}

type InvoiceService struct {
	repo           InvoiceRepository
	settingService *SettingService
}

func NewInvoiceService(repo InvoiceRepository, settingService *SettingService) *InvoiceService {
	return &InvoiceService{repo: repo, settingService: settingService}
}

func (s *InvoiceService) ListProfiles(ctx context.Context, userID int64) ([]InvoiceProfile, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	return s.repo.ListProfiles(ctx, userID)
}

func (s *InvoiceService) CreateProfile(ctx context.Context, userID int64, input InvoiceProfileInput) (*InvoiceProfile, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	normalized, err := normalizeInvoiceProfileInput(input)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateProfile(ctx, userID, normalized)
}

func (s *InvoiceService) UpdateProfile(ctx context.Context, userID, id int64, input InvoiceProfileInput) (*InvoiceProfile, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	normalized, err := normalizeInvoiceProfileInput(input)
	if err != nil {
		return nil, err
	}
	return s.repo.UpdateProfile(ctx, userID, id, normalized)
}

func (s *InvoiceService) DeleteProfile(ctx context.Context, userID, id int64) error {
	if err := s.ensureEnabled(ctx); err != nil {
		return err
	}
	return s.repo.DeleteProfile(ctx, userID, id)
}

func (s *InvoiceService) SetDefaultProfile(ctx context.Context, userID, id int64) (*InvoiceProfile, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	return s.repo.SetDefaultProfile(ctx, userID, id)
}

func (s *InvoiceService) ListEligibleSources(ctx context.Context, userID int64, page, pageSize int) ([]InvoiceEligibleSource, int64, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, 0, err
	}
	return s.repo.ListEligibleSources(ctx, userID, page, pageSize)
}

func (s *InvoiceService) CreateRequest(ctx context.Context, userID int64, input InvoiceRequestInput) (*InvoiceRequest, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	normalized, err := normalizeInvoiceRequestInput(input)
	if err != nil {
		return nil, err
	}
	requestNo, err := newInvoiceRequestNo()
	if err != nil {
		return nil, fmt.Errorf("generate invoice request no: %w", err)
	}
	return s.repo.CreateRequest(ctx, userID, normalized, requestNo)
}

func (s *InvoiceService) ListMine(ctx context.Context, userID int64, params InvoiceRequestListParams) ([]InvoiceRequest, int64, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, 0, err
	}
	return s.repo.ListRequestsByUser(ctx, userID, params)
}

func (s *InvoiceService) GetMine(ctx context.Context, userID, id int64) (*InvoiceRequest, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	return s.repo.GetRequestByUser(ctx, userID, id)
}

func (s *InvoiceService) CancelMine(ctx context.Context, userID, id int64) (*InvoiceRequest, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	return s.repo.CancelRequest(ctx, userID, id)
}

func (s *InvoiceService) AdminList(ctx context.Context, params InvoiceRequestListParams) ([]InvoiceRequest, int64, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, 0, err
	}
	return s.repo.ListRequestsAdmin(ctx, params)
}

func (s *InvoiceService) AdminGet(ctx context.Context, id int64) (*InvoiceRequest, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	return s.repo.GetRequestByID(ctx, id)
}

func (s *InvoiceService) AdminIssue(ctx context.Context, id, adminUserID int64, input InvoiceIssueInput) (*InvoiceRequest, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	input.InvoiceNumber = strings.TrimSpace(input.InvoiceNumber)
	input.InvoiceCode = strings.TrimSpace(input.InvoiceCode)
	input.InvoiceFileURL = strings.TrimSpace(input.InvoiceFileURL)
	input.InvoiceFileName = strings.TrimSpace(input.InvoiceFileName)
	input.AdminNote = strings.TrimSpace(input.AdminNote)
	if input.InvoiceNumber == "" && input.InvoiceFileURL == "" {
		return nil, infraerrors.BadRequest("INVOICE_ISSUE_INFO_REQUIRED", "invoice number or invoice file url is required")
	}
	return s.repo.IssueRequest(ctx, id, adminUserID, input)
}

func (s *InvoiceService) AdminReject(ctx context.Context, id, adminUserID int64, reason, adminNote string) (*InvoiceRequest, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	adminNote = strings.TrimSpace(adminNote)
	if reason == "" {
		return nil, infraerrors.BadRequest("INVOICE_REJECT_REASON_REQUIRED", "reject reason is required")
	}
	return s.repo.RejectRequest(ctx, id, adminUserID, reason, adminNote)
}

func (s *InvoiceService) ensureEnabled(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return ErrInvoiceManagementDisabled
	}
	if s.settingService == nil || !s.settingService.IsInvoiceManagementEnabled(ctx) {
		return ErrInvoiceManagementDisabled
	}
	return nil
}

func normalizeInvoiceProfileInput(input InvoiceProfileInput) (InvoiceProfileInput, error) {
	req := InvoiceRequestInput{
		InvoiceType:       input.InvoiceType,
		TitleName:         input.TitleName,
		TaxID:             input.TaxID,
		RegisteredAddress: input.RegisteredAddress,
		RegisteredPhone:   input.RegisteredPhone,
		BankName:          input.BankName,
		BankAccount:       input.BankAccount,
		RecipientEmail:    input.RecipientEmail,
		RecipientPhone:    input.RecipientPhone,
		SourceRefs:        []InvoiceSourceRef{{SourceType: InvoiceSourceTypePaymentOrder, SourceID: 1}},
	}
	normalized, err := normalizeInvoiceRequestInput(req)
	if err != nil {
		return InvoiceProfileInput{}, err
	}
	return InvoiceProfileInput{
		InvoiceType:       normalized.InvoiceType,
		TitleName:         normalized.TitleName,
		TaxID:             normalized.TaxID,
		RegisteredAddress: normalized.RegisteredAddress,
		RegisteredPhone:   normalized.RegisteredPhone,
		BankName:          normalized.BankName,
		BankAccount:       normalized.BankAccount,
		RecipientEmail:    normalized.RecipientEmail,
		RecipientPhone:    normalized.RecipientPhone,
		IsDefault:         input.IsDefault,
	}, nil
}

func normalizeInvoiceRequestInput(input InvoiceRequestInput) (InvoiceRequestInput, error) {
	input.InvoiceType = strings.TrimSpace(input.InvoiceType)
	input.TitleName = strings.TrimSpace(input.TitleName)
	input.TaxID = strings.TrimSpace(input.TaxID)
	input.RegisteredAddress = strings.TrimSpace(input.RegisteredAddress)
	input.RegisteredPhone = strings.TrimSpace(input.RegisteredPhone)
	input.BankName = strings.TrimSpace(input.BankName)
	input.BankAccount = strings.TrimSpace(input.BankAccount)
	input.RecipientEmail = strings.TrimSpace(input.RecipientEmail)
	input.RecipientPhone = strings.TrimSpace(input.RecipientPhone)

	if _, ok := invoiceBuyerType(input.InvoiceType); !ok {
		return InvoiceRequestInput{}, ErrInvoiceTypeInvalid
	}
	if input.TitleName == "" || input.RecipientEmail == "" {
		return InvoiceRequestInput{}, ErrInvoiceFieldRequired
	}
	if _, err := mail.ParseAddress(input.RecipientEmail); err != nil {
		return InvoiceRequestInput{}, ErrInvoiceEmailInvalid
	}
	switch input.InvoiceType {
	case InvoiceTypePersonalNormal:
		input.TaxID = ""
		input.RegisteredAddress = ""
		input.RegisteredPhone = ""
		input.BankName = ""
		input.BankAccount = ""
	case InvoiceTypeEnterpriseNormal:
		if input.TaxID == "" {
			return InvoiceRequestInput{}, ErrInvoiceFieldRequired
		}
		input.RegisteredAddress = ""
		input.RegisteredPhone = ""
		input.BankName = ""
		input.BankAccount = ""
	case InvoiceTypeEnterpriseSpecial:
		if input.TaxID == "" || input.RegisteredAddress == "" || input.RegisteredPhone == "" || input.BankName == "" || input.BankAccount == "" {
			return InvoiceRequestInput{}, ErrInvoiceFieldRequired
		}
	}
	if len(input.SourceRefs) == 0 {
		return InvoiceRequestInput{}, ErrInvoiceSourceRequired
	}
	seen := make(map[string]struct{}, len(input.SourceRefs))
	for i, ref := range input.SourceRefs {
		ref.SourceType = strings.TrimSpace(ref.SourceType)
		if ref.SourceID <= 0 || !isInvoiceSourceType(ref.SourceType) {
			return InvoiceRequestInput{}, ErrInvoiceSourceInvalid
		}
		key := fmt.Sprintf("%s:%d", ref.SourceType, ref.SourceID)
		if _, ok := seen[key]; ok {
			return InvoiceRequestInput{}, ErrInvoiceSourceInvalid
		}
		seen[key] = struct{}{}
		input.SourceRefs[i] = ref
	}
	return input, nil
}

func invoiceBuyerType(invoiceType string) (string, bool) {
	switch invoiceType {
	case InvoiceTypePersonalNormal:
		return InvoiceBuyerTypePersonal, true
	case InvoiceTypeEnterpriseNormal, InvoiceTypeEnterpriseSpecial:
		return InvoiceBuyerTypeEnterprise, true
	default:
		return "", false
	}
}

func isInvoiceSourceType(sourceType string) bool {
	return sourceType == InvoiceSourceTypePaymentOrder || sourceType == InvoiceSourceTypeRedeemCode
}

func normalizeInvoiceAmount(amount float64) (float64, bool) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return 0, false
	}
	rounded := math.Round(amount*100) / 100
	if rounded <= 0 {
		return 0, false
	}
	return rounded, true
}

func newInvoiceRequestNo() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return "INV" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(suffix[:])), nil
}

func IsInvoiceRequestNotFound(err error) bool {
	return errors.Is(err, ErrInvoiceRequestNotFound)
}
