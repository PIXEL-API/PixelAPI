package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	SystemNoticeSourcePaymentOrder = "payment_order"
	SystemNoticeSourceSubscription = "subscription"
	SystemNoticeSourceAccount      = "account"
	SystemNoticeSourceGroup        = "group"
	SystemNoticeSourceRiskControl  = "risk_control"
	SystemNoticeSourceAnnouncement = "announcement"
)

type SystemNoticeInput struct {
	UserID   int64
	Subject  string
	Content  string
	Type     string
	Source   string
	SourceID string
}

type SystemNoticeService struct {
	conversationService *ConversationService
}

func NewSystemNoticeService(conversationService *ConversationService) *SystemNoticeService {
	return &SystemNoticeService{conversationService: conversationService}
}

func (s *SystemNoticeService) Send(ctx context.Context, input SystemNoticeInput) (*Conversation, error) {
	if s == nil || s.conversationService == nil {
		return nil, ErrConversationInputRequired
	}
	input.UserID = normalizeNoticeUserID(input.UserID)
	input.Subject = normalizeNoticeText(input.Subject, 120)
	input.Content = normalizeNoticeContent(input.Content)
	input.Source = normalizeNoticeSource(input.Source)
	input.SourceID = normalizeNoticeSourceID(input.SourceID)
	if input.UserID <= 0 || input.Subject == "" || input.Content == "" || input.Source == "" || input.SourceID == "" {
		return nil, ErrConversationInputRequired
	}
	noticeType := normalizeConversationType(input.Type)
	if !isValidConversationType(noticeType) {
		noticeType = ConversationTypeNotice
	}
	out, err := s.conversationService.CreateSystemNoticeInternal(ctx, &CreateConversationInput{
		UserID:        input.UserID,
		Subject:       input.Subject,
		Content:       input.Content,
		Type:          noticeType,
		Source:        input.Source,
		SourceID:      input.SourceID,
		ContentFormat: ConversationContentFormatPlain,
	})
	if err != nil {
		if errors.Is(err, ErrConversationDuplicateSource) {
			return nil, err
		}
		return nil, fmt.Errorf("send system notice: %w", err)
	}
	return out, nil
}

func (s *SystemNoticeService) SendBestEffort(ctx context.Context, input SystemNoticeInput) {
	if s == nil {
		return
	}
	if _, err := s.Send(ctx, input); err != nil && !errors.Is(err, ErrConversationDuplicateSource) {
		slog.Warn("system_notice.send_failed", "user_id", input.UserID, "source", input.Source, "source_id", input.SourceID, "error", err)
	}
}

func (s *SystemNoticeService) NotifyPaymentOrder(ctx context.Context, event string, order *dbent.PaymentOrder) {
	if order == nil || order.UserID <= 0 {
		return
	}
	sourceID := noticeSourceID(order.ID, event)
	subject := "订单状态更新"
	content := fmt.Sprintf("你的%s订单 #%d 已更新：%s。", paymentOrderTypeLabel(order.OrderType), order.ID, paymentOrderEventLabel(event))
	switch event {
	case "paid":
		subject = "支付已确认"
		content = fmt.Sprintf("你的%s订单 #%d 支付已确认，系统正在处理。", paymentOrderTypeLabel(order.OrderType), order.ID)
	case "completed":
		subject = "订单已完成"
		content = fmt.Sprintf("你的%s订单 #%d 已完成。订单金额：%s。", paymentOrderTypeLabel(order.OrderType), order.ID, formatNoticeAmount(order.Amount, PaymentOrderCurrency(order)))
	case "cancelled":
		subject = "订单已取消"
		content = fmt.Sprintf("你的%s订单 #%d 已取消。", paymentOrderTypeLabel(order.OrderType), order.ID)
	case "expired":
		subject = "订单已过期"
		content = fmt.Sprintf("你的%s订单 #%d 已过期。", paymentOrderTypeLabel(order.OrderType), order.ID)
	case "fulfillment_failed":
		subject = "订单处理失败"
		content = fmt.Sprintf("你的%s订单 #%d 暂时处理失败，管理员会根据订单状态继续处理。", paymentOrderTypeLabel(order.OrderType), order.ID)
	case "refund_requested":
		subject = "退款申请已提交"
		content = fmt.Sprintf("你的订单 #%d 退款申请已提交，等待管理员处理。", order.ID)
	case "refunded":
		subject = "退款已完成"
		content = fmt.Sprintf("你的订单 #%d 退款已完成。退款金额：%s。", order.ID, formatNoticeAmount(order.RefundAmount, PaymentOrderCurrency(order)))
	case "refund_failed":
		subject = "退款处理失败"
		content = fmt.Sprintf("你的订单 #%d 退款暂时处理失败，管理员会继续核查。", order.ID)
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   order.UserID,
		Subject:  subject,
		Content:  content,
		Type:     ConversationTypeBilling,
		Source:   SystemNoticeSourcePaymentOrder,
		SourceID: sourceID,
	})
}

func (s *SystemNoticeService) NotifySubscription(ctx context.Context, event string, sub *UserSubscription, days int) {
	if sub == nil || sub.UserID <= 0 {
		return
	}
	groupName := "订阅"
	if sub.Group != nil && strings.TrimSpace(sub.Group.Name) != "" {
		groupName = strings.TrimSpace(sub.Group.Name)
	}
	subject := "订阅状态更新"
	content := fmt.Sprintf("你的订阅「%s」已更新。", groupName)
	switch event {
	case "created":
		subject = "订阅已开通"
		content = fmt.Sprintf("你的订阅「%s」已开通，有效期至 %s。", groupName, formatNoticeTime(sub.ExpiresAt))
	case "extended":
		subject = "订阅已续期"
		content = fmt.Sprintf("你的订阅「%s」已续期，有效期至 %s。", groupName, formatNoticeTime(sub.ExpiresAt))
	case "adjusted":
		subject = "订阅时长已调整"
		content = fmt.Sprintf("你的订阅「%s」时长已调整，有效期至 %s。", groupName, formatNoticeTime(sub.ExpiresAt))
		if days < 0 {
			content = fmt.Sprintf("你的订阅「%s」时长已扣减，有效期至 %s。", groupName, formatNoticeTime(sub.ExpiresAt))
		}
	case "revoked":
		subject = "订阅已撤销"
		content = fmt.Sprintf("你的订阅「%s」已被撤销。", groupName)
	case "quota_reset":
		subject = "订阅额度已重置"
		content = fmt.Sprintf("你的订阅「%s」额度窗口已重置。", groupName)
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   sub.UserID,
		Subject:  subject,
		Content:  content,
		Type:     ConversationTypeSubscription,
		Source:   SystemNoticeSourceSubscription,
		SourceID: noticeSourceID(sub.ID, noticeEventVersion(event, sub.UpdatedAt)),
	})
}

func (s *SystemNoticeService) NotifyGroupRateMultiplierChanged(ctx context.Context, userIDs []int64, group *Group, before, after float64, event string) {
	if len(userIDs) == 0 || group == nil || group.ID <= 0 || !groupRateMultiplierChanged(before, after) {
		return
	}
	groupName := safeGroupDisplayName(group)
	event = normalizeNoticeSource(event)
	if event == "" {
		event = "rate_changed"
	}
	for _, userID := range normalizeNoticeUserIDs(userIDs) {
		s.SendBestEffort(ctx, SystemNoticeInput{
			UserID:   userID,
			Subject:  "分组倍率已调整",
			Content:  fmt.Sprintf("你使用的分组「%s」计费倍率已由 %s 调整为 %s，后续使用将按新倍率计费。", groupName, formatNoticeRateMultiplier(before), formatNoticeRateMultiplier(after)),
			Type:     ConversationTypeBilling,
			Source:   SystemNoticeSourceGroup,
			SourceID: noticeSourceID(group.ID, noticeEventVersion(event, group.UpdatedAt)),
		})
	}
}

func (s *SystemNoticeService) NotifyUserGroupRateChanged(ctx context.Context, userID int64, group *Group, before, after *float64) {
	if userID <= 0 || group == nil || group.ID <= 0 || !noticeOptionalRatesChanged(before, after) {
		return
	}
	groupName := safeGroupDisplayName(group)
	event := "user_rate_cleared"
	subject := "专属分组倍率已清除"
	content := fmt.Sprintf("你的分组「%s」专属计费倍率已清除，将按分组默认倍率计费。", groupName)
	if after != nil {
		event = "user_rate_changed"
		subject = "专属分组倍率已调整"
		if before == nil {
			content = fmt.Sprintf("你的分组「%s」专属计费倍率已设置为 %s。", groupName, formatNoticeRateMultiplier(*after))
		} else {
			content = fmt.Sprintf("你的分组「%s」专属计费倍率已由 %s 调整为 %s。", groupName, formatNoticeRateMultiplier(*before), formatNoticeRateMultiplier(*after))
		}
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   userID,
		Subject:  subject,
		Content:  content,
		Type:     ConversationTypeBilling,
		Source:   SystemNoticeSourceGroup,
		SourceID: noticeSourceID(group.ID, noticeEventVersion(event, time.Now().UTC())),
	})
}

func (s *SystemNoticeService) NotifyAccountCreated(ctx context.Context, account *Account) {
	ownerID, ok := noticeAccountOwnerID(account)
	if !ok {
		return
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   ownerID,
		Subject:  "账号已添加",
		Content:  fmt.Sprintf("你的%s账号「%s」已添加。", accountPlatformLabel(account.Platform), safeAccountDisplayName(account)),
		Type:     ConversationTypeAccount,
		Source:   SystemNoticeSourceAccount,
		SourceID: noticeSourceID(account.ID, "created"),
	})
}

func (s *SystemNoticeService) NotifyAccountDeleted(ctx context.Context, account *Account) {
	ownerID, ok := noticeAccountOwnerID(account)
	if !ok {
		return
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   ownerID,
		Subject:  "账号已删除",
		Content:  fmt.Sprintf("你的%s账号「%s」已删除。", accountPlatformLabel(account.Platform), safeAccountDisplayName(account)),
		Type:     ConversationTypeAccount,
		Source:   SystemNoticeSourceAccount,
		SourceID: noticeSourceID(account.ID, "deleted"),
	})
}

func (s *SystemNoticeService) NotifyAccountChanged(ctx context.Context, before, after *Account) {
	beforeOwnerID, beforeOK := noticeAccountOwnerID(before)
	afterOwnerID, afterOK := noticeAccountOwnerID(after)
	if beforeOK && (!afterOK || beforeOwnerID != afterOwnerID) {
		s.SendBestEffort(ctx, SystemNoticeInput{
			UserID:   beforeOwnerID,
			Subject:  "账号已移出",
			Content:  fmt.Sprintf("你的%s账号「%s」已从当前账号归属中移出。", accountPlatformLabel(before.Platform), safeAccountDisplayName(before)),
			Type:     ConversationTypeAccount,
			Source:   SystemNoticeSourceAccount,
			SourceID: noticeSourceID(before.ID, noticeEventVersion("owner_removed", afterNoticeUpdatedAt(after, before))),
		})
	}
	if afterOK && (!beforeOK || beforeOwnerID != afterOwnerID) {
		s.SendBestEffort(ctx, SystemNoticeInput{
			UserID:   afterOwnerID,
			Subject:  "账号已分配",
			Content:  fmt.Sprintf("你的%s账号「%s」已分配到当前账号归属。", accountPlatformLabel(after.Platform), safeAccountDisplayName(after)),
			Type:     ConversationTypeAccount,
			Source:   SystemNoticeSourceAccount,
			SourceID: noticeSourceID(after.ID, noticeEventVersion("owner_assigned", after.UpdatedAt)),
		})
	}
	if !afterOK {
		return
	}
	if beforeOK && beforeOwnerID != afterOwnerID {
		return
	}
	events := accountNoticeEvents(before, after)
	for _, event := range events {
		s.SendBestEffort(ctx, SystemNoticeInput{
			UserID:   afterOwnerID,
			Subject:  event.Subject,
			Content:  event.Content,
			Type:     ConversationTypeAccount,
			Source:   SystemNoticeSourceAccount,
			SourceID: noticeSourceID(after.ID, noticeEventVersion(event.Key, after.UpdatedAt)),
		})
	}
}

func (s *SystemNoticeService) NotifyRiskControlBlocked(ctx context.Context, input ContentModerationCheckInput, decision *ContentModerationDecision) {
	if input.UserID <= 0 || decision == nil || !decision.Blocked {
		return
	}
	sourceID := strings.TrimSpace(input.RequestID)
	if sourceID == "" {
		sourceID = fmt.Sprintf("%d:%s:%s", input.UserID, input.Protocol, time.Now().UTC().Format("200601021504"))
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   input.UserID,
		Subject:  "请求已被风控拦截",
		Content:  "你的请求触发平台风控策略，已被系统拦截。请调整输入内容后重试。",
		Type:     ConversationTypeSecurity,
		Source:   SystemNoticeSourceRiskControl,
		SourceID: noticeSourceKey("blocked", sourceID),
	})
}

func (s *SystemNoticeService) NotifyRiskControlAutoBanned(ctx context.Context, userID int64) {
	if userID <= 0 {
		return
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   userID,
		Subject:  "账号已被风控限制",
		Content:  "你的账号因多次触发平台风控策略已被限制使用。请联系管理员处理。",
		Type:     ConversationTypeSecurity,
		Source:   SystemNoticeSourceRiskControl,
		SourceID: noticeSourceKey("auto_ban", time.Now().UTC().Format("20060102150405")),
	})
}

func (s *SystemNoticeService) NotifyRiskControlUnbanned(ctx context.Context, userID int64) {
	if userID <= 0 {
		return
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   userID,
		Subject:  "账号限制已解除",
		Content:  "你的账号风控限制已解除。",
		Type:     ConversationTypeSecurity,
		Source:   SystemNoticeSourceRiskControl,
		SourceID: noticeSourceKey("unban", time.Now().UTC().Format("20060102150405")),
	})
}

func (s *SystemNoticeService) NotifyAnnouncement(ctx context.Context, ann *Announcement, userID int64) {
	if ann == nil || userID <= 0 {
		return
	}
	s.SendBestEffort(ctx, SystemNoticeInput{
		UserID:   userID,
		Subject:  normalizeNoticeText(ann.Title, 120),
		Content:  normalizeNoticeContent(ann.Content),
		Type:     ConversationTypeNotice,
		Source:   SystemNoticeSourceAnnouncement,
		SourceID: noticeSourceID(ann.ID, announcementNoticeEventKey(ann)),
	})
}

func (s *SystemNoticeService) NotifyAnnouncementAudience(ctx context.Context, ann *Announcement, users []User) {
	if ann == nil || ann.NotifyMode != AnnouncementNotifyModePopup || !ann.IsActiveAt(time.Now()) {
		return
	}
	for i := range users {
		s.NotifyAnnouncement(ctx, ann, users[i].ID)
	}
}

func (s *SystemNoticeService) NotifyAnnouncementActiveUsers(ctx context.Context, ann *Announcement, userRepo UserRepository, userSubRepo UserSubscriptionRepository) {
	if s == nil || ann == nil || userRepo == nil || userSubRepo == nil || ann.NotifyMode != AnnouncementNotifyModePopup || !ann.IsActiveAt(time.Now()) {
		return
	}
	params := pagination.PaginationParams{Page: 1, PageSize: 100}
	for {
		users, page, err := userRepo.ListWithFilters(ctx, params, UserListFilters{Status: StatusActive})
		if err != nil {
			slog.Warn("system_notice.announcement_list_users_failed", "announcement_id", ann.ID, "error", err)
			return
		}
		for i := range users {
			u := users[i]
			subs, err := userSubRepo.ListActiveByUserID(ctx, u.ID)
			if err != nil {
				slog.Warn("system_notice.announcement_list_subscriptions_failed", "announcement_id", ann.ID, "user_id", u.ID, "error", err)
				continue
			}
			activeGroupIDs := make(map[int64]struct{}, len(subs))
			for j := range subs {
				activeGroupIDs[subs[j].GroupID] = struct{}{}
			}
			if ann.Targeting.Matches(u.Balance, activeGroupIDs) {
				s.NotifyAnnouncement(ctx, ann, u.ID)
			}
		}
		if page == nil || len(users) == 0 || int64(params.Page*params.PageSize) >= page.Total {
			return
		}
		params.Page++
	}
}

type accountNoticeEvent struct {
	Key     string
	Subject string
	Content string
}

func accountNoticeEvents(before, after *Account) []accountNoticeEvent {
	if before == nil || after == nil {
		return nil
	}
	name := safeAccountDisplayName(after)
	events := make([]accountNoticeEvent, 0, 2)
	beforeLevel := NormalizeAccountLevel(before.AccountLevel)
	afterLevel := NormalizeAccountLevel(after.AccountLevel)
	if beforeLevel != afterLevel {
		events = append(events, accountNoticeEvent{
			Key:     "level_changed",
			Subject: "账号等级已变更",
			Content: fmt.Sprintf("你的账号「%s」等级已由 %s 调整为 %s。", name, accountLevelLabel(beforeLevel), accountLevelLabel(afterLevel)),
		})
	}
	beforeGroups := normalizeNoticeGroupIDs(before.GroupIDs)
	afterGroups := normalizeNoticeGroupIDs(after.GroupIDs)
	if !int64SlicesEqual(beforeGroups, afterGroups) {
		events = append(events, accountNoticeEvent{
			Key:     "groups_changed",
			Subject: "账号分组已变更",
			Content: fmt.Sprintf("你的账号「%s」分组绑定已更新。", name),
		})
	}
	return events
}

func noticeAccountOwnerID(account *Account) (int64, bool) {
	if account == nil || account.OwnerUserID == nil || *account.OwnerUserID <= 0 {
		return 0, false
	}
	return *account.OwnerUserID, true
}

func normalizeNoticeUserIDs(userIDs []int64) []int64 {
	if len(userIDs) == 0 {
		return nil
	}
	out := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		userID = normalizeNoticeUserID(userID)
		if userID <= 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		out = append(out, userID)
	}
	return out
}

func normalizeNoticeUserID(userID int64) int64 {
	if userID <= 0 {
		return 0
	}
	return userID
}

func normalizeNoticeSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	source = strings.ReplaceAll(source, " ", "_")
	source = strings.ReplaceAll(source, ":", "_")
	return normalizeNoticeText(source, 80)
}

func normalizeNoticeSourceID(sourceID string) string {
	sourceID = strings.ToLower(strings.TrimSpace(sourceID))
	replacer := strings.NewReplacer(" ", "_", ":", "_", "@", "_", "/", "_", "\\", "_", "?", "_", "&", "_", "=", "_")
	sourceID = replacer.Replace(sourceID)
	return normalizeNoticeText(sourceID, 120)
}

func normalizeNoticeText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func normalizeNoticeContent(content string) string {
	return normalizeNoticeText(redactNoticeSensitiveContent(content), 10000)
}

func redactNoticeSensitiveContent(content string) string {
	replacer := strings.NewReplacer(
		"sk-", "[redacted]-",
		"access_token", "access_[redacted]",
		"refresh_token", "refresh_[redacted]",
		"api_key", "api_[redacted]",
		"password", "[redacted]",
		"secret", "[redacted]",
		"PaymentTradeNo", "Payment[redacted]",
		"payment_trade_no", "payment_[redacted]",
		"OutTradeNo", "OrderNo",
		"out_trade_no", "order_no",
	)
	return replacer.Replace(content)
}

func noticeSourceID(id int64, event string) string {
	return normalizeNoticeSourceID(fmt.Sprintf("%d_%s", id, normalizeNoticeSource(event)))
}

func noticeEventVersion(event string, updatedAt time.Time) string {
	event = normalizeNoticeSource(event)
	if updatedAt.IsZero() {
		return event
	}
	return event + "_" + strconv.FormatInt(updatedAt.UnixNano(), 10)
}

func noticeSourceKey(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = normalizeNoticeSourceID(part)
		if part == "" {
			continue
		}
		normalized = append(normalized, part)
	}
	return normalizeNoticeText(strings.Join(normalized, "_"), 120)
}

func afterNoticeUpdatedAt(after, before *Account) time.Time {
	if after != nil && !after.UpdatedAt.IsZero() {
		return after.UpdatedAt
	}
	if before != nil {
		return before.UpdatedAt
	}
	return time.Time{}
}

func formatNoticeAmount(amount float64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "CNY"
	}
	return fmt.Sprintf("%s %s", strconv.FormatFloat(amount, 'f', 2, 64), currency)
}

func formatNoticeTime(t time.Time) string {
	if t.IsZero() {
		return "未设置"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func formatNoticeRateMultiplier(rate float64) string {
	text := strconv.FormatFloat(rate, 'f', 6, 64)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" || text == "-0" {
		text = "0"
	}
	return text + "x"
}

func paymentOrderTypeLabel(orderType string) string {
	switch orderType {
	case payment.OrderTypeSubscription:
		return "订阅"
	case payment.OrderTypeShop:
		return "商品"
	default:
		return "充值"
	}
}

func paymentOrderEventLabel(event string) string {
	switch event {
	case "paid":
		return "支付已确认"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	case "expired":
		return "已过期"
	case "refund_requested":
		return "已提交退款申请"
	case "refunded":
		return "已退款"
	case "refund_failed":
		return "退款失败"
	default:
		return "状态已更新"
	}
}

func accountPlatformLabel(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return ""
	}
	return platform + " "
}

func safeAccountDisplayName(account *Account) string {
	if account == nil {
		return "账号"
	}
	name := normalizeNoticeText(account.Name, 80)
	if name == "" {
		return fmt.Sprintf("#%d", account.ID)
	}
	return name
}

func safeGroupDisplayName(group *Group) string {
	if group == nil {
		return "分组"
	}
	name := normalizeNoticeText(group.Name, 80)
	if name == "" {
		return "分组"
	}
	return name
}

func noticeOptionalRatesChanged(before, after *float64) bool {
	if before == nil && after == nil {
		return false
	}
	if before == nil || after == nil {
		return true
	}
	return groupRateMultiplierChanged(*before, *after)
}

func groupRateMultiplierChanged(before, after float64) bool {
	diff := before - after
	if diff < 0 {
		diff = -diff
	}
	return diff > 0.0000001
}

func accountLevelLabel(level string) string {
	switch NormalizeAccountLevel(level) {
	case AccountLevelFree:
		return "Free"
	case AccountLevelPlus:
		return "Plus"
	case AccountLevelPro:
		return "Pro"
	case AccountLevelTeam:
		return "Team"
	default:
		return "Unknown"
	}
}

func normalizeNoticeGroupIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
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
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func int64SlicesEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func announcementNoticeEventKey(ann *Announcement) string {
	if ann == nil {
		return "published"
	}
	if !ann.UpdatedAt.IsZero() {
		return "published:" + strconv.FormatInt(ann.UpdatedAt.Unix(), 10)
	}
	return "published"
}
