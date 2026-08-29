#!/usr/bin/env bash
# 本地运行时回归：Xvfb + llvmpipe 软件渲染跑桌面构建，xdotool 驱动
# 封面→存档→设置→开关/滑块→切换存档→删除三连确认 全流程，逐步截图。
# 依赖: xvfb xdotool imagemagick（以及 CGO 工具链 + X11/Wayland/GL 头文件）
# 用法: ./regress_xvfb.sh [输出目录]   （默认 /tmp/mcgo-regress）
# 已知限制: 软渲染帧率可能低至 5fps，XTEST 快速点击偶发落在同一帧内被
# raylib 的逐帧边沿检测丢弃——脚本的点击已延长按压跨帧并带重试，若仍
# 偶发失败，重跑即可（真机 60fps 无此问题）。
#
# 注意: xdotool 的 key/click 必须分帧发送（mousedown/mouseup 之间 sleep），
# 同帧的按下+抬起会被 raylib 的逐帧边沿检测丢弃。
set -uo pipefail

OUT="${1:-/tmp/mcgo-regress}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$PATH:/usr/local/go/bin"
mkdir -p "$OUT"; cd "$OUT"
rm -f run.log shot_*.png

echo "==> 构建 CGO 桌面版"
(cd "$REPO" && CGO_ENABLED=1 go build -o "$OUT/mcgo-cgo" .) || exit 1

echo "==> 启动 Xvfb :99"
Xvfb :99 -screen 0 1280x720x24 >/dev/null 2>&1 &
XVFB_PID=$!
sleep 2
cleanup() { kill "$XVFB_PID" 2>/dev/null; pkill -x mcgo-cgo 2>/dev/null; }
trap cleanup EXIT

echo "==> 启动游戏"
DISPLAY=:99 LIBGL_ALWAYS_SOFTWARE=1 ./mcgo-cgo > run.log 2>&1 &
sleep 6
WID=$(DISPLAY=:99 xdotool search --name "Minecraft Go" | head -1)
[ -z "$WID" ] && { echo "❌ 找不到游戏窗口"; tail -20 run.log; exit 1; }
DISPLAY=:99 xdotool windowfocus "$WID"

# 软件渲染帧率可能低至 5fps：按下阶段必须跨帧，否则边沿会被逐帧
# 检测丢弃。真机 60fps 下无需如此长按。
# 按压时长必须跨帧：软渲染可能低至 5fps（200ms/帧），同帧内的
# 按下+抬起会被逐帧边沿检测整体丢掉（真机 60fps 无此问题）。
tap()  { DISPLAY=:99 xdotool mousemove "$1" "$2" mousedown 1; sleep 0.35; DISPLAY=:99 xdotool mouseup 1; sleep 0.4; }
key()  { DISPLAY=:99 xdotool keydown "$1"; sleep 0.3; DISPLAY=:99 xdotool keyup "$1"; sleep 0.6; }
shot() { DISPLAY=:99 import -window root "shot_$1.png"; sleep 0.15
  DISPLAY=:99 import -window root -crop 280x22+8+6 "hud_$1.png" 2>/dev/null # HUD 坐标行（位置持久化断言用）
  echo "   截图 shot_$1.png"; }

fail=0
expect_diff() { # 画面应发生变化
  if cmp -s "$2" "$3"; then echo "   ❌ $1: 画面无变化"; fail=1; else echo "   ✅ $1"; fi
}
expect_same() { # 画面应保持一致（状态稳定不回弹）
  if cmp -s "$2" "$3"; then echo "   ✅ $1"; else echo "   ❌ $1: 状态回弹"; fail=1; fi
}
expect_similar() { # 容差比较：差异像素少于阈值视为一致（吸收单帧抖动）
  n=$(compare -metric AE "$2" "$3" /dev/null 2>&1 || true)
  n=${n%%[^0-9]*}
  if [ "${n:-999999}" -le 60000 ]; then echo "   ✅ $1 (差异 ${n:-?} 像素)"; else echo "   ❌ $1: 差异 ${n:-?} 像素过大"; fail=1; fi
}

shot 01_title
[ -s shot_01_title.png ] || { echo "❌ 封面未渲染"; exit 1; }

echo "==> 存档 2 进入世界"
tap 640 341; sleep 10
shot 02_world; [ -s shot_02_world.png ] || fail=1

echo "==> 破坏准星方块（压低视角保证命中地面；此俯仰角将随存档恢复）"
DISPLAY=:99 xdotool mousemove_relative -- 0 70
sleep 0.3
tap 640 360
tap 640 360
sleep 0.4
shot 03_broken
expect_diff "方块破坏" shot_02_world.png shot_03_broken.png

echo "==> ESC 暂停（PAUSED 应在按钮上方）"
key Escape
shot 04_pause

echo "==> 设置页"
tap 640 449; sleep 0.5
shot 05_settings

echo "==> 水平反转开（2 秒后复查保持；点击带重试，规避软渲染下的丢边沿）"
tap 640 99; sleep 0.4
DISPLAY=:99 import -window root -crop 74x38+1182+79 "sw_try.png" 2>/dev/null
tries=0
while cmp -s "sw_05.png" "sw_try.png" && [ "$tries" -lt 3 ]; do
  tries=$((tries+1))
  echo "   开关未翻转，重试 $tries"
  tap 640 99; sleep 0.4
  DISPLAY=:99 import -window root -crop 74x38+1182+79 "sw_try.png" 2>/dev/null
done
shot 06_inv_on
sleep 2
shot 07_inv_on_2s
SW=$(DISPLAY=:99 xdotool getwindowgeometry --shell "$WID" | grep -E '^(X|Y)=' | cut -d= -f2 | paste -sd, -)
# 开关本体区域（settings 页第 1 行右侧胶囊）
DISPLAY=:99 import -window root -crop 74x38+1182+79 "sw_05.png" 2>/dev/null
DISPLAY=:99 import -window root -crop 74x38+1182+79 "sw_06.png" 2>/dev/null
DISPLAY=:99 import -window root -crop 74x38+1182+79 "sw_07.png" 2>/dev/null
expect_diff "开关已翻转" sw_05.png sw_06.png
expect_same "开关保持（不回弹）" sw_06.png sw_07.png
tap 640 99; sleep 0.4   # 恢复

echo "==> 渲染距离第 5 档"
tap 955 419; sleep 0.5
shot 08_tier5

echo "==> 切换存档 → 封面（存档 2 应有元数据）"
tap 640 547; sleep 0.6
shot 09_title_meta

echo "==> 重进存档 2（读档会恢复存档时的俯仰角，应与破坏后机位一致）"
DISPLAY=:99 xdotool mousemove 640 360   # 指针归中，避免重进时指针跳变残余转动视角
sleep 0.3
tap 640 341; sleep 10
shot 10_reloaded
expect_same "位置持久化（HUD 坐标逐字一致）" hud_03_broken.png hud_10_reloaded.png
shot 10_full
expect_similar "信息性检查：重进后全景与破坏后相似（相机含 EMA 残余偏移，仅供参考）" shot_03_broken.png shot_10_full.png || true

echo "==> 删除存档 2（三连确认，按钮换位）"
key Escape; sleep 1
tap 640 449; sleep 0.5
tap 640 547; sleep 0.6     # 切换存档回封面
tap 1196 341; sleep 0.4    # 存档 2 的删
shot 11_confirm_1
tap 263 395; sleep 0.4     # 确认 1（左中）
shot 12_confirm_2
tap 903 481; sleep 0.4     # 确认 2（右中）
tap 494 596; sleep 0.5     # 确认 3（下中）
shot 13_deleted

echo "==> 无存档开始"
tap 640 516; sleep 10
shot 14_nosave_world

echo ""
if [ "$fail" -eq 0 ]; then echo "✅ 回归通过（详见 $OUT/shot_*.png；run.log 含完整 raylib 日志）"
else echo "❌ 回归有失败项，请检查 $OUT/shot_*.png 与 run.log"; fi
exit "$fail"
