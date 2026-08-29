package main

import (
	_ "embed"

	rl "github.com/gen2brain/raylib-go/raylib"
)

// 菜单中文字体：wqy-microhei 按 assets/uichars.txt 子集化（约 27KB），
// 字符清单与子集来自同一文件，保证不缺字。默认字体只有 ASCII。
var (
	//go:embed assets/menu_font.ttf
	menuFontData []byte
	//go:embed assets/uichars.txt
	uiCharsRaw string
)

var (
	menuFont   rl.Font
	menuFontOK bool
)

// loadMenuFont 在 InitWindow 之后调用一次。
func loadMenuFont() {
	if menuFontOK {
		return
	}
	seen := map[rune]bool{}
	cps := []rune{}
	for r := rune(32); r <= 126; r++ {
		cps = append(cps, r)
		seen[r] = true
	}
	for _, r := range uiCharsRaw {
		if !seen[r] && r > 32 {
			cps = append(cps, r)
			seen[r] = true
		}
	}
	menuFont = rl.LoadFontFromMemory(".ttf", menuFontData, 48, cps)
	rl.SetTextureFilter(menuFont.Texture, rl.FilterBilinear)
	menuFontOK = true
}

func drawTextC(text string, x, y, size float32, c rl.Color) {
	rl.DrawTextEx(menuFont, text, rl.NewVector2(x, y), size, 0, c)
}

func drawTextCCentered(text string, cx, y, size float32, c rl.Color) {
	w := measureTextC(text, size)
	rl.DrawTextEx(menuFont, text, rl.NewVector2(cx-w/2, y), size, 0, c)
}

func measureTextC(text string, size float32) float32 {
	return rl.MeasureTextEx(menuFont, text, size, 0).X
}
