// +build windows

package minssh

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type (
	short int16
	word  uint16
	dword uint32
	wchar uint16

	coord struct {
		x short
		y short
	}
	smallRect struct {
		left   short
		top    short
		right  short
		bottom short
	}
	consoleScreenBufferInfo struct {
		size              coord
		cursorPosition    coord
		attributes        word
		window            smallRect
		maximumWindowSize coord
	}
	inputRecord struct {
		eventType word
		_         word     // Padding. Event struct is aligned Dword
		event     [16]byte // union struct's largest bytes
	}
	keyEventRecord struct {
		keyDown         int32
		repeatCount     word
		virtualKeyCode  word
		virtualScanCode word
		unicodeChar     wchar
		controlKeyState dword
	}
	mouseEventRecord struct {
		mousePosition   coord
		buttonState     dword
		controlKeyState dword
		eventFlags      dword
	}
	windowBufferSizeRecord struct {
		size coord
	}
)

const (
	enableEchoInput            = 0x0004
	enableExtendedFlags        = 0x0080
	enableInsertMode           = 0x0020
	enableLineInput            = 0x0002
	enableMouseInput           = 0x0010
	enableProcessedInput       = 0x0001
	enableQuickEditMode        = 0x0040
	enableWindowInput          = 0x0008
	enableAutoPosition         = 0x0100 // not in doc but it is said available
	enableVirtualTerminalInput = 0x0200

	enableProcessedOutput           = 0x0001
	enableWrapAtEolOutput           = 0x0002
	enableVirtualTerminalProcessing = 0x0004
	disableNewlineAutoReturn        = 0x0008
	enableLvbGridWorldwide          = 0x0010

	focusEvent            = 0x0010
	keyEvent              = 0x0001
	menuEvent             = 0x0008
	mouseEvent            = 0x0002
	windowBufferSizeEvent = 0x0004

	errorAccessDenied     syscall.Errno = 5
	errorInvalidHandle    syscall.Errno = 6
	errorInvalidParameter syscall.Errno = 87
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var (
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procReadConsoleInput           = kernel32.NewProc("ReadConsoleInputW")
	procAttachConsole              = kernel32.NewProc("AttachConsole")
	procAllocConsole               = kernel32.NewProc("AllocConsole")
)

type sysInfo struct {
	stdinMode  dword
	stdoutMode dword
	stderrMode dword
	emuStdin   bool
	emuStdout  bool
	lastRune   rune
}

func getConsoleMode(fd uintptr) (mode dword, err error) {
	r1, _, e1 := syscall.Syscall(procGetConsoleMode.Addr(), 2, fd, uintptr(unsafe.Pointer(&mode)), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}

	return
}

func setConsoleMode(fd uintptr, mode dword) (err error) {
	r1, _, e1 := syscall.Syscall(procSetConsoleMode.Addr(), 2, fd, uintptr(mode), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}

	return
}

func getConsoleScreenBufferInfo() (info consoleScreenBufferInfo, err error) {
	r1, _, e1 := syscall.Syscall(procGetConsoleScreenBufferInfo.Addr(), 2, os.Stdout.Fd(), uintptr(unsafe.Pointer(&info)), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}

	return
}

func readConsoleInput(fd uintptr, records []inputRecord) (n dword, err error) {
	if len(records) == 0 {
		return 0, nil
	}

	r1, _, e1 := syscall.Syscall6(procReadConsoleInput.Addr(), 4, fd, uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&n)), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}

	return
}

func attachConsole(pid dword) (err error) {
	r1, _, e1 := syscall.Syscall(procAttachConsole.Addr(), 1, uintptr(pid), 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}

	return

}

func allocConsole() (err error) {
	r1, _, e1 := syscall.Syscall(procAllocConsole.Addr(), 0, 0, 0, 0)
	if r1 == 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}

	return
}

func isTerminal(fd uintptr) bool {
	_, err := getConsoleMode(fd)
	if err != nil {
		return false
	}
	return true
}

const (
	conin  string = "CONIN$"
	conout string = "CONOUT$"
)

func openTTY() (ttyin, ttyout *os.File, err error) {
	if !isTerminal(os.Stdin.Fd()) || !isTerminal(os.Stdout.Fd()) {
		err = attachConsole(dword(os.Getpid()))
		if err != nil && err == error(errorInvalidHandle) {
			err = allocConsole()
			if err != nil {
				return nil, nil, err
			}
		}
	}

	if isTerminal(os.Stdin.Fd()) {
		ttyin = os.Stdin
	} else {
		ttyin, err = os.OpenFile(conin, os.O_RDWR, 0)
		if err != nil {
			return nil, nil, err
		}
	}

	if isTerminal(os.Stdout.Fd()) {
		ttyout = os.Stdout
	} else {
		ttyout, err = os.OpenFile(conout, os.O_RDWR, 0)
		if err != nil {
			return nil, nil, err
		}
	}
	return
}

func closeTTY(ttyin, ttyout *os.File) {
	if ttyin != os.Stdin {
		ttyin.Close()
	}
	if ttyout != os.Stdout {
		ttyout.Close()
	}
}

func (ms *MinSSH) changeLocalTerminalMode() error {
	var err error

	ms.sys.stdinMode, err = getConsoleMode(os.Stdin.Fd())
	if err != nil {
		return fmt.Errorf("failed to get local stdin mode: %s", err)
	}

	ms.sys.stdoutMode, err = getConsoleMode(os.Stdout.Fd())
	if err != nil {
		return fmt.Errorf("failed to get local stdout mode: %s", err)
	}

	ms.sys.stderrMode, err = getConsoleMode(os.Stderr.Fd())
	if err != nil {
		return fmt.Errorf("failed to get local stderr mode: %s", err)
	}

	newBaseMode := ms.sys.stdinMode &^ (enableEchoInput | enableProcessedInput | enableLineInput)
	newMode := newBaseMode | enableVirtualTerminalInput
	err = setConsoleMode(os.Stdin.Fd(), newMode)
	if err != nil {
		ms.conf.Logger.Printf("failed to set local stdin mode with 'EnableVirtualTerminalInput': %s\n", err)
		err = setConsoleMode(os.Stdin.Fd(), newBaseMode)
		if err != nil {
			return fmt.Errorf("failed to set local stdin mode: %s", err)
		}
		ms.conf.Logger.Println("stdin fallback to internal input emulator")
		ms.sys.emuStdin = true
	}

	newMode = ms.sys.stdoutMode | enableVirtualTerminalProcessing | disableNewlineAutoReturn
	err = setConsoleMode(os.Stdout.Fd(), newMode)
	if err != nil {
		ms.conf.Logger.Printf("failed to set local stdout mode with 'EnableVirtualTerminalProcessing' and 'DisableNewlineAutoReturn': %s\n", err)

		newMode = ms.sys.stdoutMode | enableVirtualTerminalProcessing
		err = setConsoleMode(os.Stdout.Fd(), newMode)
		if err != nil {
			ms.conf.Logger.Printf("failed to set local stdout mode with 'EnableVirtualTerminalProcessing': %s\n", err)
			ms.conf.Logger.Println("stdout fallback to internal output emulator")
			ms.sys.stdoutMode = 0 // don't have to restore stdout mode
			ms.sys.emuStdout = true
		}
	}

	if ms.sys.emuStdout {
		ms.conf.Logger.Println("stderr fallback to internal output emulator")
		ms.sys.stderrMode = 0
	} else {
		newMode = ms.sys.stdoutMode | enableVirtualTerminalProcessing | disableNewlineAutoReturn
		err = setConsoleMode(os.Stderr.Fd(), newMode)
		if err != nil {
			ms.conf.Logger.Printf("failed to set local stderr mode with 'EnableVirtualTerminalProcessing' and 'DisableNewlineAutoReturn': %s\n", err)

			newMode = ms.sys.stdoutMode | enableVirtualTerminalProcessing
			err = setConsoleMode(os.Stderr.Fd(), newMode)
			if err != nil {
				ms.conf.Logger.Printf("failed to set local stderr mode with 'EnableVirtualTerminalProcessing': %s\n", err)
				ms.conf.Logger.Println("stderr fallback to internal output emulator")
			}
		}
	}

	return nil
}

func (ms *MinSSH) restoreLocalTerminalMode() error {
	var inE, outE, errE error
	if ms.sys.stdinMode > 0 {
		inE = setConsoleMode(os.Stdin.Fd(), ms.sys.stdinMode)
	}
	if ms.sys.stdoutMode > 0 {
		outE = setConsoleMode(os.Stdout.Fd(), ms.sys.stdoutMode)
	}
	if ms.sys.stderrMode > 0 {
		errE = setConsoleMode(os.Stderr.Fd(), ms.sys.stderrMode)
	}
	if inE != nil || outE != nil || errE != nil {
		errs := make([]string, 0, 3)
		if inE != nil {
			errs = append(errs, fmt.Sprintf("stdin: %d", inE))
		}
		if outE != nil {
			errs = append(errs, fmt.Sprintf("stdout: %d", outE))
		}
		if errE != nil {
			errs = append(errs, fmt.Sprintf("stderr: %d", errE))
		}

		emsg := "failed to restore "
		emsg += strings.Join(errs, ", ")
		return fmt.Errorf(emsg)
	}
	return nil
}

func (ms *MinSSH) getWindowSize() (width, height int, err error) {
	info, err := getConsoleScreenBufferInfo()
	if err != nil {
		return 0, 0, err
	}
	return int(info.window.right - info.window.left + 1), int(info.window.bottom - info.window.top + 1), nil
}

func (ms *MinSSH) watchTerminalResize(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)

	ms.wg.Add(1)
	go func() {
		defer ms.wg.Done()

		ticker := time.NewTicker(2 * time.Second)
		defer func() {
			ticker.Stop()
			close(ch)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ch <- struct{}{}
			}
		}
	}()

	return ch
}

func (ms *MinSSH) readFromStdin(b []byte) (n int, err error) {
	var stdin io.Reader
	if ms.sys.emuStdin {
		stdin = NewAnsiReader(os.Stdin)
	} else {
		stdin = os.Stdin
	}
	return stdin.Read(b)
}

func (ms *MinSSH) copyToStdout() (err error) {
	var stdout io.Writer
	if ms.sys.emuStdout {
		stdout = NewAnsiWriter(os.Stdout)
	} else {
		stdout = os.Stdout
	}
	_, err = io.Copy(stdout, ms.rStdout)
	return
}

func (ms *MinSSH) copyToStderr() (err error) {
	var stderr io.Writer
	if ms.sys.emuStdout {
		stderr = NewAnsiWriter(os.Stderr)
	} else {
		stderr = os.Stderr
	}
	_, err = io.Copy(stderr, ms.rStderr)
	return
}
