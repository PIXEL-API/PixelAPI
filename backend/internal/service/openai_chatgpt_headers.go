package service

import "net/http"

func setOpenAIChatGPTAccountHeaders(headers http.Header, account *Account) {
	if headers == nil || account == nil {
		return
	}
	if accountID := account.GetChatGPTAccountID(); accountID != "" {
		headers.Set("chatgpt-account-id", accountID)
	}
}
