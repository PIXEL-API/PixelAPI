package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestConversationRepositoryRejectsInvalidScopedInputsBeforeDB(t *testing.T) {
	repo := NewConversationRepository(nil)

	err := repo.CreateWithMessage(context.Background(), nil, nil)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.AddMessage(context.Background(), 0, &service.ConversationMessage{}, service.ConversationStatusOpen)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.GetByID(context.Background(), 0)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.GetByIDForUser(context.Background(), 0, 1)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.GetSystemNoticeBySource(context.Background(), 0, "payment_order", "1:completed")
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.GetSystemNoticeBySource(context.Background(), 1, "", "1:completed")
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.GetSystemNoticeBySource(context.Background(), 1, "payment_order", "")
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, _, err = repo.ListForUser(context.Background(), 0, pagination.PaginationParams{Page: 1, PageSize: 20}, service.ConversationListFilters{})
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, _, err = repo.ListMessages(context.Background(), 0, pagination.PaginationParams{Page: 1, PageSize: 20}, service.ConversationMessageListFilters{})
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, _, err = repo.ListMessages(context.Background(), 1, pagination.PaginationParams{Page: 1, PageSize: 20}, service.ConversationMessageListFilters{BeforeID: -1})
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.MarkRead(context.Background(), 0, service.ConversationSenderTypeUser, nil)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.UpdateStatus(context.Background(), 0, service.ConversationStatusClosed)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.UpdateAssignee(context.Background(), 0, nil)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)

	_, err = repo.CountUnreadForUser(context.Background(), 0)
	require.ErrorIs(t, err, service.ErrConversationInputRequired)
}
