// Package save 实现三个存档槽位的读写与删除。存档内容 = 世界种子 +
// 玩家状态 + 全部方块修改。文件位于应用私有目录（save1.bin..save3.bin）。
package save

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"mc-go/world"
)

const magic = uint32(0x4D435331) // "MCS1"

// PlayerState 是读档恢复所需的玩家状态子集。
type PlayerState struct {
	X, Y, Z     float32
	Yaw, Pitch  float32
	Flying      bool
	Selected    uint8
}

// Data 是一个存档槽的完整内容。
type Data struct {
	Seed      int64
	LastSaved int64 // unix 秒
	Player    PlayerState
	Edits     []world.EditRecord
}

// Meta 是封面上展示的槽位摘要。
type Meta struct {
	Exists    bool
	Seed      int64
	LastSaved int64
	Edits     int
}

// Path 返回槽位文件路径。
func Path(appDir string, slot int) string {
	return fmt.Sprintf("%s/save%d.bin", appDir, slot)
}

// GetMeta 返回槽位摘要（文件不存在时 Exists=false）。
func GetMeta(appDir string, slot int) Meta {
	data, err := os.ReadFile(Path(appDir, slot))
	if err != nil || len(data) < 48 || binary.LittleEndian.Uint32(data[0:4]) != magic {
		return Meta{}
	}
	return Meta{
		Exists:    true,
		Seed:      int64(binary.LittleEndian.Uint64(data[5:13])),
		LastSaved: int64(binary.LittleEndian.Uint64(data[37:45])),
		Edits:     int(binary.LittleEndian.Uint32(data[45:49])),
	}
}

// Exists 报告槽位是否有存档。
func Exists(appDir string, slot int) bool {
	_, err := os.Stat(Path(appDir, slot))
	return err == nil
}

// Write 写入存档（尽力而为）。
func Write(appDir string, slot int, d Data) {
	edits := d.Edits
	buf := make([]byte, 49+len(edits)*13)
	binary.LittleEndian.PutUint32(buf[0:4], magic)
	buf[4] = 1 // version
	binary.LittleEndian.PutUint64(buf[5:13], uint64(d.Seed))
	binary.LittleEndian.PutUint32(buf[13:17], f2b(d.Player.X))
	binary.LittleEndian.PutUint32(buf[17:21], f2b(d.Player.Y))
	binary.LittleEndian.PutUint32(buf[21:25], f2b(d.Player.Z))
	binary.LittleEndian.PutUint32(buf[25:29], f2b(d.Player.Yaw))
	binary.LittleEndian.PutUint32(buf[29:33], f2b(d.Player.Pitch))
	if d.Player.Flying {
		buf[33] = 1
	}
	buf[34] = d.Player.Selected
	buf[35] = 0 // reserved
	buf[36] = 0 // reserved
	binary.LittleEndian.PutUint64(buf[37:45], uint64(d.LastSaved))
	binary.LittleEndian.PutUint32(buf[45:49], uint32(len(edits)))
	for i, e := range edits {
		off := 49 + 13*i
		binary.LittleEndian.PutUint32(buf[off:off+4], uint32(e.X))
		binary.LittleEndian.PutUint32(buf[off+4:off+8], uint32(e.Y))
		binary.LittleEndian.PutUint32(buf[off+8:off+12], uint32(e.Z))
		buf[off+12] = e.Block
	}
	_ = os.WriteFile(Path(appDir, slot), buf, 0644)
}

// Read 读取存档内容。
func Read(appDir string, slot int) (Data, error) {
	data, err := os.ReadFile(Path(appDir, slot))
	if err != nil {
		return Data{}, err
	}
	if len(data) < 49 || binary.LittleEndian.Uint32(data[0:4]) != magic {
		return Data{}, fmt.Errorf("bad save file: %s", Path(appDir, slot))
	}
	var d Data
	d.Seed = int64(binary.LittleEndian.Uint64(data[5:13]))
	d.Player.X = b2f(data[13:17])
	d.Player.Y = b2f(data[17:21])
	d.Player.Z = b2f(data[21:25])
	d.Player.Yaw = b2f(data[25:29])
	d.Player.Pitch = b2f(data[29:33])
	d.Player.Flying = data[33] == 1
	d.Player.Selected = data[34]
	d.LastSaved = int64(binary.LittleEndian.Uint64(data[37:45]))
	count := int(binary.LittleEndian.Uint32(data[45:49]))
	if count > 1<<20 {
		count = 1 << 20 // 防御损坏文件
	}
	for i := 0; i < count; i++ {
		off := 49 + 13*i
		if off+13 > len(data) {
			break
		}
		d.Edits = append(d.Edits, world.EditRecord{
			X:     int32(binary.LittleEndian.Uint32(data[off : off+4])),
			Y:     int32(binary.LittleEndian.Uint32(data[off+4 : off+8])),
			Z:     int32(binary.LittleEndian.Uint32(data[off+8 : off+12])),
			Block: data[off+12],
		})
	}
	return d, nil
}

// Delete 删除槽位存档（不存在时静默）。
func Delete(appDir string, slot int) {
	_ = os.Remove(Path(appDir, slot))
}

func f2b(v float32) uint32 { return math.Float32bits(v) }
func b2f(b []byte) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b)) }
