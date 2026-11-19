package settings

type IrisConfig struct {
	BaseURL                 string
	BaseURLFile             string
	WebhookToken            string
	BotToken                string
	Transport               string
	H3CACertFile            string
	H3ServerName            string
	BaseURLAllowedHosts     []string
	ValidateBaseURLFileStat bool
}
