package irisx

import "strings"

// ResolveToken: 개별 토큰(웹훅/봇) 값을 정규화합니다.
// sharedToken 인자는 하위 호환성을 위해 유지되지만 더 이상 사용하지 않습니다.
func ResolveToken(token, sharedToken string) string {
	_ = sharedToken
	return strings.TrimSpace(token)
}

// ResolveTokens: webhook/bot 토큰 값을 각각 정규화합니다.
// sharedToken 인자는 하위 호환성을 위해 유지되지만 더 이상 사용하지 않습니다.
func ResolveTokens(webhookToken, botToken, sharedToken string) (resolvedWebhookToken, resolvedBotToken string) {
	return ResolveToken(webhookToken, sharedToken), ResolveToken(botToken, sharedToken)
}
