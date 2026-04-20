//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userHandlerRepoStub struct {
	user *service.User
}

func (s *userHandlerRepoStub) Create(context.Context, *service.User) error { return nil }
func (s *userHandlerRepoStub) GetByID(context.Context, int64) (*service.User, error) {
	cloned := *s.user
	return &cloned, nil
}
func (s *userHandlerRepoStub) GetByEmail(context.Context, string) (*service.User, error) {
	cloned := *s.user
	return &cloned, nil
}
func (s *userHandlerRepoStub) GetFirstAdmin(context.Context) (*service.User, error) {
	cloned := *s.user
	return &cloned, nil
}
func (s *userHandlerRepoStub) Update(_ context.Context, user *service.User) error {
	cloned := *user
	s.user = &cloned
	return nil
}
func (s *userHandlerRepoStub) Delete(context.Context, int64) error { return nil }
func (s *userHandlerRepoStub) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	if s.user == nil || s.user.AvatarURL == "" {
		return nil, nil
	}
	return &service.UserAvatar{
		StorageProvider: s.user.AvatarSource,
		URL:             s.user.AvatarURL,
		ContentType:     s.user.AvatarMIME,
		ByteSize:        s.user.AvatarByteSize,
		SHA256:          s.user.AvatarSHA256,
	}, nil
}
func (s *userHandlerRepoStub) UpsertUserAvatar(_ context.Context, _ int64, input service.UpsertUserAvatarInput) (*service.UserAvatar, error) {
	s.user.AvatarURL = input.URL
	s.user.AvatarSource = input.StorageProvider
	s.user.AvatarMIME = input.ContentType
	s.user.AvatarByteSize = input.ByteSize
	s.user.AvatarSHA256 = input.SHA256
	return &service.UserAvatar{
		StorageProvider: input.StorageProvider,
		URL:             input.URL,
		ContentType:     input.ContentType,
		ByteSize:        input.ByteSize,
		SHA256:          input.SHA256,
	}, nil
}
func (s *userHandlerRepoStub) DeleteUserAvatar(context.Context, int64) error {
	s.user.AvatarURL = ""
	s.user.AvatarSource = ""
	s.user.AvatarMIME = ""
	s.user.AvatarByteSize = 0
	s.user.AvatarSHA256 = ""
	return nil
}
func (s *userHandlerRepoStub) List(context.Context, pagination.PaginationParams) ([]service.User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *userHandlerRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, service.UserListFilters) ([]service.User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *userHandlerRepoStub) UpdateBalance(context.Context, int64, float64) error { return nil }
func (s *userHandlerRepoStub) DeductBalance(context.Context, int64, float64) error { return nil }
func (s *userHandlerRepoStub) UpdateConcurrency(context.Context, int64, int) error { return nil }
func (s *userHandlerRepoStub) ExistsByEmail(context.Context, string) (bool, error) { return false, nil }
func (s *userHandlerRepoStub) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	return 0, nil
}
func (s *userHandlerRepoStub) AddGroupToAllowedGroups(context.Context, int64, int64) error {
	return nil
}
func (s *userHandlerRepoStub) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	return nil
}
func (s *userHandlerRepoStub) UpdateTotpSecret(context.Context, int64, *string) error { return nil }
func (s *userHandlerRepoStub) EnableTotp(context.Context, int64) error                { return nil }
func (s *userHandlerRepoStub) DisableTotp(context.Context, int64) error               { return nil }

func TestUserHandlerUpdateProfileReturnsAvatarURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &userHandlerRepoStub{
		user: &service.User{
			ID:       11,
			Email:    "handler-avatar@example.com",
			Username: "handler-avatar",
			Role:     service.RoleUser,
			Status:   service.StatusActive,
		},
	}
	handler := NewUserHandler(service.NewUserService(repo, nil, nil, nil), nil, nil)

	body := []byte(`{"avatar_url":"https://cdn.example.com/avatar.png"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/user", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})

	handler.UpdateProfile(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AvatarURL string `json:"avatar_url"`
			Username  string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "https://cdn.example.com/avatar.png", resp.Data.AvatarURL)
	require.Equal(t, "handler-avatar", resp.Data.Username)
}
