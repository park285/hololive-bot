package render

import "github.com/kapu/hololive-api/internal/planes/bot/internal/render/theme"

var (
	palette       = theme.Minimal()
	colWhite      = palette.Background
	colSlate100   = palette.SurfaceMuted
	colSlate200   = palette.Border
	colSlate500   = palette.TextMuted
	colSlate800   = palette.TextPrimary
	colAmber50    = palette.AccentWarmBg
	colAmber600   = palette.AccentWarm
	colEmerald50  = palette.AccentCoolBg
	colEmerald600 = palette.AccentCool
)
