// +build !windows,!plan9,!nacl

package minssh

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

const (
	pathTTY string = "/dev/tty"
)

type sysInfo struct {
	origMode *terminal.State
}

func openTTY() (ttyin, ttyout *os.File, err error) {
	if terminal.IsTerminal(int(os.Stdin.Fd())) {
		ttyin = os.Stdin
	} else {
		ttyin, err = os.OpenFile(pathTTY, os.O_RDWR, 0)
		if err != nil {
			return nil, nil, err
		}
	}
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		ttyout = os.Stdout
	} else {
		ttyout, err = os.OpenFile(pathTTY, os.O_RDWR, 0)
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

func (ms *MinSSH) changeLocalTerminalMode() (err error) {
	if ms.sys.origMode, err = terminal.MakeRaw(int(os.Stdin.Fd())); err != nil {
		return fmt.Errorf("failed to set stdin to raw mode: %s", err)
	}

	return nil
}

func (ms *MinSSH) restoreLocalTerminalMode() error {
	if ms.sys.origMode != nil {
		return terminal.Restore(int(os.Stdin.Fd()), ms.sys.origMode)
	}
	return nil
}

func (ms *MinSSH) getWindowSize() (width, height int, err error) {
	return terminal.GetSize(int(os.Stdin.Fd()))
}

func (ms *MinSSH) watchTerminalResize(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGWINCH)

	ms.wg.Add(1)
	go func() {
		defer func() {
			signal.Reset(syscall.SIGWINCH)
			signal.Stop(sigC)
			close(ch)
			ms.wg.Done()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-sigC:
				ch <- struct{}{}
			}
		}
	}()

	return ch
}

func (ms *MinSSH) readFromStdin(b []byte) (n int, err error) {
	return os.Stdin.Read(b)
}

func (ms *MinSSH) copyToStdout() (err error) {
	_, err = io.Copy(os.Stdout, ms.rStdout)
	return
}

func (ms *MinSSH) copyToStderr() (err error) {
	_, err = io.Copy(os.Stderr, ms.rStderr)
	return
}
