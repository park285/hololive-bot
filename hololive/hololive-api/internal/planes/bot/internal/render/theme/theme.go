package theme

import "image/color"

type Palette struct {
	Background   color.RGBA
	SurfaceMuted color.RGBA
	Border       color.RGBA
	TextMuted    color.RGBA
	TextPrimary  color.RGBA
	AccentWarm   color.RGBA
	AccentWarmBg color.RGBA
	AccentCool   color.RGBA
	AccentCoolBg color.RGBA
}

func Minimal() Palette {
	return Palette{
		Background:   color.RGBA{R: 255, G: 255, B: 255, A: 255},
		SurfaceMuted: color.RGBA{R: 241, G: 245, B: 249, A: 255},
		Border:       color.RGBA{R: 226, G: 232, B: 240, A: 255},
		TextMuted:    color.RGBA{R: 100, G: 116, B: 139, A: 255},
		TextPrimary:  color.RGBA{R: 30, G: 41, B: 59, A: 255},
		AccentWarm:   color.RGBA{R: 217, G: 119, B: 6, A: 255},
		AccentWarmBg: color.RGBA{R: 255, G: 251, B: 235, A: 255},
		AccentCool:   color.RGBA{R: 5, G: 150, B: 105, A: 255},
		AccentCoolBg: color.RGBA{R: 236, G: 253, B: 245, A: 255},
	}
}
