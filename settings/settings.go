// Package settings 保存触屏操作与视角偏好，序列化到应用私有目录的一个
// 小文件（安卓为 internalDataPath，桌面为当前目录）。布局覆盖以
// "屏幕比例 + 相对缩放"存储，跨分辨率可复用。
package settings

import (
	"encoding/binary"
	"math"
	"os"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const storageMagic = uint32(0x4D434631) // "MCF1"

// Path 由宿主在启动时设置（SetPath），Load/Save 使用。
var Path = "settings.bin"

// SetPath 设置持久化文件路径。
func SetPath(p string) { Path = p }

// 按钮的稳定键名，与序列化顺序一一对应。
var ButtonOrder = []string{"jump", "fly", "descend", "break", "place"}

// Btn 是单个按钮的布局覆盖：归一化中心坐标（0..1）与相对缩放。
type Btn struct {
	NX, NY  float32
	Scale   float32
}

// Settings 是全部可持久化的用户偏好。
type Settings struct {
	InvertX, InvertY bool
	AutoJump         bool

	SensX, SensY float32 // 灵敏度倍率 0.2..3.0

	FreeJoystick bool
	AnchorSet    bool
	AnchorX, AnchorY float32 // 固定摇杆锚点（归一化，限左半屏）

	HotbarScaleSet bool
	HotbarScale    float32 // 0.5..2.0

	RenderTier int // 渲染距离档位 1..5，1 为默认

	// Buttons 键存在于 map 即表示用户自定义过该按钮。
	Buttons map[string]Btn
}

// Current 是进程内唯一的设置实例。
var Current = Defaults()

// Defaults 返回默认设置。
func Defaults() *Settings {
	return &Settings{SensX: 1, SensY: 1, HotbarScale: 1, RenderTier: 1}
}

// ClampTier 把渲染距离档位限制到 1..5。
func ClampTier(v int) int {
	if v < 1 {
		return 1
	}
	if v > 5 {
		return 5
	}
	return v
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClampSens 把灵敏度限制到有效范围。
func ClampSens(v float32) float32 { return clamp(v, 0.2, 3.0) }

// ClampScale 把按钮/物品栏缩放限制到有效范围。
func ClampScale(v float32) float32 { return clamp(v, 0.5, 2.0) }

// ClampAnchorX/Y 把固定摇杆锚点限制在左半屏内。
func ClampAnchorX(v float32) float32 { return clamp(v, 0.04, 0.46) }
func ClampAnchorY(v float32) float32 { return clamp(v, 0.08, 0.92) }

// SetButtonRect 记录按钮的屏幕矩形（连同基础尺寸），换算为归一化覆盖。
func SetButtonRect(name string, rect rl.Rectangle, w, h float32) {
	if Current.Buttons == nil {
		Current.Buttons = map[string]Btn{}
	}
	Current.Buttons[name] = Btn{
		NX:    (rect.X + rect.Width/2) / w,
		NY:    (rect.Y + rect.Height/2) / h,
		Scale: ClampScale(rect.Width / buttonBaseSize(h)),
	}
}

// ButtonBaseSize 返回按钮的基础边长（与 buildLayout 的公式一致）。
func buttonBaseSize(h float32) float32 {
	scale := h / 720
	if scale < 0.75 {
		scale = 0.75
	}
	if scale > 3 {
		scale = 3
	}
	size := float32(76) * scale
	if size < 84 {
		size = 84
	}
	return size
}

// ResetLayout 清除全部布局覆盖（保留灵敏度/反转/自动跳跃）。
func ResetLayout() {
	Current.FreeJoystick = false
	Current.AnchorSet = false
	Current.HotbarScaleSet = false
	Current.HotbarScale = 1
	Current.Buttons = nil
}

func f2b(buf []byte, v float32) { binary.LittleEndian.PutUint32(buf, math.Float32bits(v)) }
func b2f(buf []byte) float32   { return math.Float32frombits(binary.LittleEndian.Uint32(buf)) }

// Load 从磁盘读取设置；没有存档时保持默认。
func Load() {
	Current = Defaults()
	data, err := os.ReadFile(Path)
	if err != nil || len(data) != 108 || binary.LittleEndian.Uint32(data[0:4]) != storageMagic {
		return
	}
	s := Defaults()
	flags := binary.LittleEndian.Uint32(data[4:8])
	s.InvertX = flags&(1<<0) != 0
	s.InvertY = flags&(1<<1) != 0
	s.AutoJump = flags&(1<<2) != 0
	s.FreeJoystick = flags&(1<<3) != 0
	s.AnchorSet = flags&(1<<4) != 0
	s.HotbarScaleSet = flags&(1<<5) != 0
	s.RenderTier = ClampTier(int(flags>>8) & 7)
	s.SensX = ClampSens(b2f(data[8:12]))
	s.SensY = ClampSens(b2f(data[12:16]))
	s.AnchorX = ClampAnchorX(b2f(data[16:20]))
	s.AnchorY = ClampAnchorY(b2f(data[20:24]))
	s.HotbarScale = ClampScale(b2f(data[24:28]))
	s.Buttons = map[string]Btn{}
	for i, name := range ButtonOrder {
		off := 28 + 12*i
		nx := b2f(data[off : off+4])
		if math.IsNaN(float64(nx)) {
			continue // 该按钮未自定义
		}
		s.Buttons[name] = Btn{
			NX:    nx,
			NY:    b2f(data[off+4 : off+8]),
			Scale: ClampScale(b2f(data[off+8 : off+12])),
		}
	}
	if len(s.Buttons) == 0 {
		s.Buttons = nil
	}
	Current = s
}

// Save 把当前设置写入磁盘（尽力而为，失败静默）。
func Save() {
	buf := make([]byte, 108)
	binary.LittleEndian.PutUint32(buf[0:4], storageMagic)
	var flags uint32
	if Current.InvertX {
		flags |= 1 << 0
	}
	if Current.InvertY {
		flags |= 1 << 1
	}
	if Current.AutoJump {
		flags |= 1 << 2
	}
	if Current.FreeJoystick {
		flags |= 1 << 3
	}
	if Current.AnchorSet {
		flags |= 1 << 4
	}
	if Current.HotbarScaleSet {
		flags |= 1 << 5
	}
	flags |= uint32(Current.RenderTier&7) << 8
	binary.LittleEndian.PutUint32(buf[4:8], flags)
	f2b(buf[8:12], Current.SensX)
	f2b(buf[12:16], Current.SensY)
	f2b(buf[16:20], Current.AnchorX)
	f2b(buf[20:24], Current.AnchorY)
	f2b(buf[24:28], Current.HotbarScale)
	for i, name := range ButtonOrder {
		off := 28 + 12*i
		if b, ok := Current.Buttons[name]; ok {
			f2b(buf[off:off+4], b.NX)
			f2b(buf[off+4:off+8], b.NY)
			f2b(buf[off+8:off+12], b.Scale)
		} else {
			f2b(buf[off:off+4], float32(math.NaN()))
		}
	}
	_ = os.WriteFile(Path, buf, 0644)
}
