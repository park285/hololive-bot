package irisx

import "strings"

// ResolveToken: 개별 토큰(웹훅/봇)이 비어있으면 sharedToken으로 대체합니다.
func ResolveToken(token, sharedToken string) string {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken != "" {
		return trimmedToken
	}
	return strings.TrimSpace(sharedToken)
}

// ResolveTokens: webhook/bot 토큰을 sharedToken 기준으로 보정합니다.
func ResolveTokens(webhookToken, botToken, sharedToken string) (resolvedWebhookToken, resolvedBotToken string) {
	return ResolveToken(webhookToken, sharedToken), ResolveToken(botToken, sharedToken)
}
