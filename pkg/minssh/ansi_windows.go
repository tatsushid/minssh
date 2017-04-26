// +build windows

package minssh

import (
	"fmt"
	"io"
	"os"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	ansiterm "github.com/Azure/go-ansiterm"
	"github.com/Azure/go-ansiterm/winterm"
)

// virtual key codes from
// https://msdn.microsoft.com/ja-jp/library/windows/desktop/dd375731.aspx
const (
	vk_lbutton = 1 + iota
	vk_rbutton
	vk_cancel
	vk_mbutton
	vk_xbutton1
	vk_xbutton2
	_ // undefined
	vk_back
	vk_tab
	_ // reserved
	_ // reserved
	vk_clear
	vk_return
	_ // undefined
	_ // undefined
	vk_shift
	vk_control
	vk_menu
	vk_pause
	vk_capital
	vk_kana // or vk_hanguel, vk_hangul
	_
	vk_junja
	vk_final
	vk_hanja // or vk_kanji
	_        // undefined
	vk_escape
	vk_convert
	vk_nonconvert
	vk_accept
	vk_modechange
	vk_space
	vk_prior
	vk_next
	vk_end
	vk_home
	vk_left
	vk_up
	vk_right
	vk_down
	vk_select
	vk_print
	vk_execute
	vk_snapshot
	vk_insert
	vk_delete
	vk_help
	// numbers and alphabets
)

const (
	vk_lwin = 0x5B + iota
	vk_rwin
	vk_apps
	_
	vk_sleep
	vk_numpad0
	vk_numpat1
	vk_numpat2
	vk_numpat3
	vk_numpat4
	vk_numpat5
	vk_numpat6
	vk_numpat7
	vk_numpat8
	vk_numpat9
	vk_multiply
	vk_add
	vk_separator
	vk_subtract
	vk_decimal
	vk_divide
	vk_f1
	vk_f2
	vk_f3
	vk_f4
	vk_f5
	vk_f6
	vk_f7
	vk_f8
	vk_f9
	vk_f10
	vk_f11
	vk_f12
	vk_f13
	vk_f14
	vk_f15
	vk_f16
	vk_f17
	vk_f18
	vk_f19
	vk_f20
	vk_f21
	vk_f22
	vk_f23
	vk_f24
	_ // unassigned
	_ // unassigned
	vk_numlock
	vk_scroll
	// unassigned and OEM specific
)

const (
	vk_lshift = 0xA0 + iota
	vk_rshift
	vk_lcontrol
	vk_rcontrol
	vk_lmenu
	vk_rmenu
	vk_browser_back
	vk_browser_forward
	vk_browser_refresh
	vk_browser_stop
	vk_browser_search
	vk_browser_favorites
	vk_browser_home
	vk_volume_mute
	vk_volume_down
	vk_volume_up
	vk_media_next_track
	vk_media_prev_track
	vk_media_stop
	vk_media_play_pause
	vk_launch_mail
	vk_launch_media_select
	vk_launch_app1
	vk_launch_app2
	_ // reserved
	_ // reserved
	vk_oem_1
	vk_oem_plus
	vk_oem_comma
	vk_oem_minus
	vk_oem_period
	vk_oem_2
	vk_oem_3
	// reserved and unassigned
)

const (
	vk_oem_4 = 0xDB + iota
	vk_oem_5
	vk_oem_6
	vk_oem_7
	vk_oem_8
	_ // reserved
	_ // OEM specific
	vk_oem_102
	_ // OEM specific
	_ // OEM specific
	vk_processkey
	_ // OEM specific
	vk_packet
	// unassigned and OEM specific
)

const (
	vk_attn = 0xF6 + iota
	vk_crsel
	vk_exsel
	vk_ereof
	vk_play
	vk_zoom
	vk_noname
	vk_pa1
	vk_oem_clear
)

// control key states from
// https://msdn.microsoft.com/ja-jp/library/windows/desktop/ms684166(v=vs.85).aspx
const (
	capslock_on        = 0x0080
	enhanced_key       = 0x0100
	left_alt_pressed   = 0x0002
	left_ctrl_pressed  = 0x0008
	numlock_on         = 0x0020
	right_alt_pressed  = 0x0001
	right_ctrl_pressed = 0x0004
	scrolllock_on      = 0x0040
	shift_pressed      = 0x0010
)

var arrowKeyMap = map[int]byte{
	vk_up:    'A',
	vk_down:  'B',
	vk_right: 'C',
	vk_left:  'D',
	vk_home:  'H',
	vk_end:   'F',
}

var f1ToF4KeyMap = map[int]byte{
	vk_f1: 'P',
	vk_f2: 'Q',
	vk_f3: 'R',
	vk_f4: 'S',
}

var f5toF12KeyMap = map[int][2]byte{
	vk_f5:  [2]byte{'1', '5'},
	vk_f6:  [2]byte{'1', '7'},
	vk_f7:  [2]byte{'1', '8'},
	vk_f8:  [2]byte{'1', '9'},
	vk_f9:  [2]byte{'2', '0'},
	vk_f10: [2]byte{'2', '1'},
	vk_f11: [2]byte{'2', '3'},
	vk_f12: [2]byte{'2', '4'},
}

type ansiReader struct {
	fd       uintptr
	file     *os.File
	lastRune rune
}

func NewAnsiReader(f *os.File) *ansiReader {
	return &ansiReader{fd: f.Fd(), file: f, lastRune: 0}
}

func (ar *ansiReader) Read(b []byte) (n int, err error) {
	var nr, i dword
	records := make([]inputRecord, 2)

	nr, err = readConsoleInput(ar.fd, records)
	if err != nil {
		return 0, err
	}
	if nr == 0 {
		return 0, io.EOF
	}

	b = b[:0]
	for i = 0; i < nr; i++ {
		record := records[i]
		switch record.eventType {
		case keyEvent:
			ev := (*keyEventRecord)(unsafe.Pointer(&record.event[0]))
			if ev.keyDown != int32(1) {
				continue // only need keydown event, ignore keyup
			}

			if seq := ar.toAnsiSeq(ev); seq != nil {
				b = append(b, seq...)
				continue
			}

			r := rune(ev.unicodeChar)
			if ar.lastRune != 0 {
				r = utf16.DecodeRune(ar.lastRune, r)
				ar.lastRune = 0
			} else if utf16.IsSurrogate(r) {
				ar.lastRune = r // save half for next time
				continue
			}

			if r == 0 {
				continue
			}
			ne := utf8.EncodeRune(b[len(b):cap(b)], r)
			b = b[:len(b)+ne]
		case mouseEvent, windowBufferSizeEvent, focusEvent, menuEvent:
			// just ignore, do nothing
		default:
			// unknown event, do nothing
		}
	}

	return len(b), nil
}

func (ar *ansiReader) Close() error {
	return ar.file.Close()
}

func (ar *ansiReader) Fd() uintptr {
	return ar.fd
}

// toAnsiSeq converts a key event to a ansi sequence based on "Input Sequences"
// section at
// https://msdn.microsoft.com/ja-jp/library/windows/desktop/mt638032(v=vs.85).aspx
func (ar *ansiReader) toAnsiSeq(k *keyEventRecord) []byte {
	switch vk := int(k.virtualKeyCode); vk {
	case vk_up, vk_down, vk_right, vk_left, vk_home, vk_end:
		if k.controlKeyState&(left_ctrl_pressed|right_ctrl_pressed) != 0 {
			return []byte(fmt.Sprintf("\033[1;5%c", arrowKeyMap[vk]))
		} else {
			return []byte(fmt.Sprintf("\033[%c", arrowKeyMap[vk]))
		}
	case vk_back:
		if k.controlKeyState&(left_alt_pressed|right_alt_pressed) != 0 &&
			k.controlKeyState&(left_ctrl_pressed|right_ctrl_pressed) == 0 {
			return []byte{0x1b, '\b'}
		}
		return []byte{0x7f}
	case vk_pause:
		return []byte{0x1a}
	case vk_escape:
		return []byte{0x1b}
	case vk_insert:
		return []byte(fmt.Sprintf("\033[2~"))
	case vk_delete:
		return []byte(fmt.Sprintf("\033[3~"))
	case vk_prior:
		return []byte(fmt.Sprintf("\033[5~"))
	case vk_next:
		return []byte(fmt.Sprintf("\033[6~"))
	case vk_f1, vk_f2, vk_f3, vk_f4:
		return []byte(fmt.Sprintf("\033O%c", f1ToF4KeyMap[vk]))
	case vk_f5, vk_f6, vk_f7, vk_f8, vk_f9, vk_f10, vk_f11, vk_f12:
		return []byte(fmt.Sprintf("\033[%c%c~", f5toF12KeyMap[vk][0], f5toF12KeyMap[vk][1]))
	}

	if 0x20 <= k.unicodeChar && k.unicodeChar <= 0x7E &&
		k.controlKeyState&(left_alt_pressed|right_alt_pressed) != 0 &&
		k.controlKeyState&(left_ctrl_pressed|right_ctrl_pressed) == 0 {
		return []byte(fmt.Sprintf("\033%c", k.unicodeChar))
	}
	return nil
}

type ansiWriter struct {
	fd     uintptr
	file   *os.File
	parser *ansiterm.AnsiParser
}

func NewAnsiWriter(f *os.File) *ansiWriter {
	aw := &ansiWriter{fd: f.Fd(), file: f}
	aw.parser = ansiterm.CreateParser("Ground", winterm.CreateWinEventHandler(f.Fd(), f))
	return aw
}

func (aw *ansiWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	return aw.parser.Parse(p)
}

func (aw *ansiWriter) Close() error {
	return aw.file.Close()
}

func (aw *ansiWriter) Fd() uintptr {
	return aw.fd
}
