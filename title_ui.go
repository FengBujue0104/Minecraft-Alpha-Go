package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"

	"mc-go/save"
)

// 封面页：标题 + 三个存档槽 + 无存档开始。删除需要在不同位置的按钮上
// 连续确认三次，防止误触。点击结果通过返回值交给主循环（pendingSlot）。

type titleState struct {
	appDir      string
	confirmSlot int // -1 无；否则为正在三连确认删除的槽位
	confirmStep int // 0..2
	sessionLive bool
	metas       [3]save.Meta
}

func (t *titleState) refresh(appDir string, sessionLive bool) {
	t.appDir = appDir
	t.sessionLive = sessionLive
	t.confirmSlot = -1
	t.confirmStep = 0
	for i := 0; i < 3; i++ {
		t.metas[i] = save.GetMeta(appDir, i+1)
	}
}

// titleFrame 处理封面页的输入与绘制。返回 >=0 的槽位表示请求开局
// （0 = 无存档开始），由主循环在更新阶段执行 startSession。
func titleFrame(t *titleState, loading bool) int {
	w := float32(rl.GetScreenWidth())
	h := float32(rl.GetScreenHeight())
	s := uiScale()
	justPressed, _ := uiEdges()

	// 背景：上深下浅的竖向色带，模拟天空到地表
	rl.ClearBackground(rl.NewColor(13, 16, 24, 255))
	rl.DrawRectangle(0, 0, int32(w), int32(h*0.42), rl.NewColor(24, 34, 52, 255))
	for i, c := range []rl.Color{
		rl.NewColor(56, 142, 96, 255), rl.NewColor(48, 126, 86, 255),
		rl.NewColor(40, 110, 76, 255), rl.NewColor(34, 96, 66, 255),
	} {
		band := h*0.42 + float32(i)*(h*0.58/4)
		rl.DrawRectangle(0, int32(band), int32(w), int32(h*0.58/4)+1, c)
	}

	drawTextCCentered("MINECRAFT GO", w/2, h*0.06, 44*s, rl.White)
	drawTextCCentered("Alpha voxel sandbox · Go", w/2, h*0.06+46*s, 16*s, rl.NewColor(160, 172, 190, 255))

	if loading {
		drawTextCCentered("正在生成世界...", w/2, h/2, 22*s, rl.White)
		return -1
	}

	// 删除三连确认覆盖层
	if t.confirmSlot >= 0 {
		slot := t.confirmSlot
		rl.DrawRectangle(0, 0, int32(w), int32(h), rl.NewColor(8, 10, 16, 235))
		drawTextCCentered(fmt.Sprintf("删除存档 %d？", slot), w/2, h*0.22, 26*s, rl.White)
		drawTextCCentered("此操作不可恢复", w/2, h*0.22+34*s, 15*s, rl.NewColor(230, 120, 120, 255))
		drawTextCCentered(fmt.Sprintf("请在不同位置点击确认按钮（第 %d/3 次）", t.confirmStep+1), w/2, h*0.30, 16*s, rl.Yellow)

		// 三个确认按钮轮换出现在不同位置，逐次点击
		positions := [][2]float32{
			{w * 0.12, h * 0.5},
			{w * 0.62, h * 0.62},
			{w * 0.30, h * 0.78},
		}
		pos := positions[t.confirmStep%3]
		btn := rl.Rectangle{X: pos[0], Y: pos[1], Width: 220 * s, Height: 70 * s}
		if buttonC(btn, "确认删除", 19*s, justPressed) {
			t.confirmStep++
			if t.confirmStep >= 3 {
				save.Delete(t.appDir, slot)
				t.refresh(t.appDir, t.sessionLive)
			}
		}
		cancelBtn := rl.Rectangle{X: w/2 - 90*s, Y: h*0.88, Width: 180 * s, Height: 54 * s}
		if buttonC(cancelBtn, "取消", 17*s, justPressed) {
			t.confirmSlot = -1
			t.confirmStep = 0
		}
		return -1
	}

	// 存档槽
	rowH := 74 * s
	y := h * 0.30
	for i := 0; i < 3; i++ {
		slot := i + 1
		row := rl.Rectangle{X: w * 0.10, Y: y, Width: w * 0.80, Height: rowH}
		if buttonC(row, fmt.Sprintf("存档 %d", slot), 20*s, justPressed) {
			return slot
		}
		meta := t.metas[i]
		var sub string
		if meta.Exists {
			sub = fmt.Sprintf("种子 %d · 修改 %d 处", meta.Seed, meta.Edits)
		} else {
			sub = "空"
		}
		drawTextC(sub, row.X+18*s, y+rowH-30*s, 13*s, rl.NewColor(170, 182, 200, 220))

		delBtn := rl.Rectangle{X: row.X + row.Width + 12*s, Y: y + (rowH-52*s)/2, Width: 64 * s, Height: 52 * s}
		dark := rl.NewColor(70, 30, 30, 235)
		if buttonC(delBtn, "删", 18*s, justPressed) {
			if meta.Exists {
				t.confirmSlot = slot
				t.confirmStep = 0
			}
		}
		_ = dark
		y += rowH + 14*s
	}

	// 无存档开始
	freeBtn := rl.Rectangle{X: w * 0.10, Y: y + 6*s, Width: w * 0.80, Height: 60 * s}
	if buttonC(freeBtn, "无存档开始", 19*s, justPressed) {
		return 0
	}
	y += 60*s + 20*s

	// 会话进行中时允许返回游戏
	if t.sessionLive {
		backBtn := rl.Rectangle{X: w * 0.10, Y: y, Width: w * 0.80, Height: 52 * s}
		if buttonC(backBtn, "返回当前游戏", 17*s, justPressed) {
			return -2 // 主循环：仅退出封面（恢复 sessionActive 状态）
		}
	}

	return -1
}
