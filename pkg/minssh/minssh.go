package minssh

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/terminal"
)

const (
	defaultTermName string = "xterm"
	maxPromptTries  int    = 3
)

type MinSSH struct {
	conf *Config

	conn *ssh.Client
	sess *ssh.Session

	rStdin  io.WriteCloser
	rStdout io.Reader
	rStderr io.Reader

	sys *sysInfo

	wg sync.WaitGroup
}

func IsTerminal() (bool, error) {
	if !terminal.IsTerminal(int(os.Stdin.Fd())) || !terminal.IsTerminal(int(os.Stdout.Fd())) {
		s := "cannot run on non-terminal device."
		if runtime.GOOS == "windows" {
			s += " if you use mintty on Cygwin/MSYS, please wrap this by winpty"
		}
		return false, fmt.Errorf(s)
	}
	return true, nil
}

func readPassword(prompt string) (password string, err error) {
	state, err := terminal.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("failed to get terminal state: %s", err)
	}

	stopC := make(chan struct{})
	defer func() {
		close(stopC)
	}()

	go func() {
		sigC := make(chan os.Signal, 1)
		signal.Notify(sigC, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		select {
		case <-sigC:
			terminal.Restore(int(os.Stdin.Fd()), state)
			os.Exit(1)
		case <-stopC:
		}
	}()

	if prompt == "" {
		fmt.Print("Password: ")
	} else {
		fmt.Print(prompt)
	}

	b, err := terminal.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %s", err)
	}

	fmt.Print("\n")

	return string(b), nil
}

func askAddingUnknownHostKey(address string, remote net.Addr, key ssh.PublicKey) (bool, error) {
	stopC := make(chan struct{})
	defer func() {
		close(stopC)
	}()

	go func() {
		sigC := make(chan os.Signal, 1)
		signal.Notify(sigC, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		select {
		case <-sigC:
			os.Exit(1)
		case <-stopC:
		}
	}()

	fmt.Printf("The authenticity of host '%s (%s)' can't be established.\n", address, remote.String())
	fmt.Printf("RSA key fingerprint is %s\n", ssh.FingerprintSHA256(key))
	fmt.Printf("Are you sure you want to continue connecting (yes/no)? ")

	b := bufio.NewReader(os.Stdin)
	for {
		answer, err := b.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("failed to read answer: %s", err)
		}
		answer = string(strings.ToLower(strings.TrimSpace(answer)))
		if answer == "yes" {
			return true, nil
		} else if answer == "no" {
			return false, nil
		}
		fmt.Print("Please type 'yes' or 'no': ")
	}
	return false, nil
}

func askDecodingEncryptedKey(keyPath string) (bool, error) {
	stopC := make(chan struct{})
	defer func() {
		close(stopC)
	}()

	go func() {
		sigC := make(chan os.Signal, 1)
		signal.Notify(sigC, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		select {
		case <-sigC:
			os.Exit(1)
		case <-stopC:
		}
	}()

	fmt.Printf("%q is encrypted\n", keyPath)
	fmt.Printf("do you want to decrypt it (yes/no)? ")

	b := bufio.NewReader(os.Stdin)
	for {
		answer, err := b.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("failed to read answer: %s", err)
		}
		answer = string(strings.ToLower(strings.TrimSpace(answer)))
		if answer == "yes" {
			return true, nil
		} else if answer == "no" {
			return false, nil
		}
		fmt.Print("Please type 'yes' or 'no': ")
	}
	return false, nil
}

func Open(conf *Config) (ms *MinSSH, err error) {
	ms = &MinSSH{conf: conf, sys: &sysInfo{}}

	config := &ssh.ClientConfig{
		User: ms.conf.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(ms.getSigners),
			ssh.RetryableAuthMethod(ssh.KeyboardInteractive(ms.keyboardInteractiveChallenge), maxPromptTries),
			ssh.RetryableAuthMethod(ssh.PasswordCallback(ms.passwordCallback), maxPromptTries),
		},
		HostKeyCallback: ms.verifyAndAppendNew,
	}

	if ms.conn, err = ssh.Dial("tcp", ms.Hostport(), config); err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %s", ms.Hostport(), err)
	}

	if ms.sess, err = ms.conn.NewSession(); err != nil {
		return nil, fmt.Errorf("cannot create session: %s", err)
	}

	return ms, nil
}

func (ms *MinSSH) verifyAndAppendNew(hostname string, remote net.Addr, key ssh.PublicKey) error {
	if len(ms.conf.KnownHostsFiles) == 0 {
		return fmt.Errorf("there is no knownhosts file")
	}

	hostKeyCallback, err := knownhosts.New(ms.conf.KnownHostsFiles...)
	if err != nil {
		return fmt.Errorf("failed to load knownhosts files: %s", err)
	}

	err = hostKeyCallback(hostname, remote, key)
	if err == nil {
		return nil
	}

	keyErr, ok := err.(*knownhosts.KeyError)
	if !ok || len(keyErr.Want) > 0 {
		return err
	}

	if answer, err := askAddingUnknownHostKey(hostname, remote, key); err != nil || !answer {
		msg := "host key verification failed"
		if err != nil {
			msg += ": " + err.Error()
		}
		return fmt.Errorf(msg)
	}

	f, err := os.OpenFile(ms.conf.KnownHostsFiles[0], os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to add new host key: %s", err)
	}
	defer f.Close()

	var addrs []string
	if remote.String() == hostname {
		addrs = []string{hostname}
	} else {
		addrs = []string{hostname, remote.String()}
	}

	entry := knownhosts.Line(addrs, key)
	if _, err = f.WriteString(entry + "\n"); err != nil {
		return fmt.Errorf("failed to add new host key: %s", err)
	}

	return nil
}

func (ms *MinSSH) getSigners() (signers []ssh.Signer, err error) {
	for _, identityFile := range ms.conf.IdentityFiles {
		identityFile = os.ExpandEnv(identityFile)
		key, err := ioutil.ReadFile(identityFile)
		if err != nil {
			ms.conf.Logger.Printf("failed to read private key %q: %s\n", identityFile, err)
			continue
		}
		block, _ := pem.Decode(key)
		if x509.IsEncryptedPEMBlock(block) {
			if answer, err := askDecodingEncryptedKey(identityFile); err != nil || !answer {
				if err != nil {
					ms.conf.Logger.Printf("failed to decrypt private key: %s\n", err)
				} else {
					ms.conf.Logger.Printf("cancel decrypting private key\n")
				}
				continue
			}
			password, err := readPassword("password for decrypting key: ")
			if err != nil {
				ms.conf.Logger.Printf("failed to decrypt private key: %s\n", err)
				continue
			}
			block.Bytes, err = x509.DecryptPEMBlock(block, []byte(password))
			if err != nil {
				ms.conf.Logger.Printf("failed to decrypt private key: %s\n", err)
				continue
			}
			block.Headers = make(map[string]string)
			key = pem.EncodeToMemory(block)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			ms.conf.Logger.Printf("failed to parse private key: %s\n", err)
			continue
		}
		signers = append(signers, signer)
	}

	return signers, nil
}

func (ms *MinSSH) keyboardInteractiveChallenge(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
	answers = make([]string, len(questions))
	var strs []string
	if len(questions) > 0 {
		if user != "" {
			strs = append(strs)
		}
		if instruction != "" {
			strs = append(strs)
		}
		if len(strs) > 0 {
			fmt.Println(strings.Join(strs, " "))
		} else {
			fmt.Printf("Keyboard interactive challenge for %s@%s\n", ms.conf.User, ms.conf.Host)
		}
	}
	for i, q := range questions {
		res, err := readPassword(q)
		if err != nil {
			return answers, err
		}
		answers[i] = res
	}
	return answers, err
}

func (ms *MinSSH) passwordCallback() (secret string, err error) {
	fmt.Printf("Password authentication for %s@%s\n", ms.conf.User, ms.conf.Host)
	return readPassword("Password: ")
}

func (ms *MinSSH) Close() {
	err := ms.restoreLocalTerminalMode()
	if err != nil {
		ms.conf.Logger.Println(err)
	}
	if ms.sess != nil {
		ms.sess.Close()
	}
	if ms.conn != nil {
		ms.conn.Close()
	}
}

func (ms *MinSSH) Hostport() string {
	return fmt.Sprintf("%s:%d", ms.conf.Host, ms.conf.Port)
}

func (ms *MinSSH) prepareRemoteTerminal() (err error) {
	termName := os.Getenv("TERM")
	if termName == "" {
		termName = defaultTermName
	}

	w, h, err := ms.getWindowSize()
	if err != nil {
		return fmt.Errorf("failed to get terminal width and height: %s", err)
	}

	if !ms.conf.NoTTY {
		if err = ms.sess.RequestPty(termName, h, w, ssh.TerminalModes{}); err != nil {
			return fmt.Errorf("request for pseudo terminal failed: %s", err)
		}
	}

	if ms.rStdin, err = ms.sess.StdinPipe(); err != nil {
		return fmt.Errorf("failed to get remote stdin pipe: %s", err)
	}

	if ms.rStdout, err = ms.sess.StdoutPipe(); err != nil {
		return fmt.Errorf("failed to get remote stdout pipe: %s", err)
	}

	if ms.rStderr, err = ms.sess.StderrPipe(); err != nil {
		return fmt.Errorf("failed to get remote stderr pipe: %s", err)
	}

	if err = ms.sess.Shell(); err != nil {
		return fmt.Errorf("failed to start shell: %s", err)
	}

	return nil
}

func (ms *MinSSH) prepareLocalTerminal() (err error) {
	if err = ms.changeLocalTerminalMode(); err != nil {
		return fmt.Errorf("failed to change local terminal mode: %s", err)
	}

	return nil
}

func (ms *MinSSH) watchSignals() chan os.Signal {
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	return sigC
}

type windowChangeReq struct {
	W, H, Wpx, Hpx uint32
}

func (ms *MinSSH) invokeResizeTerminal(ctx context.Context) {
	ch := ms.watchTerminalResize(ctx)

	ms.wg.Add(1)
	go func() {
		defer ms.wg.Done()

		w, h, err := ms.getWindowSize()
		if err != nil {
			ms.conf.Logger.Printf("failed to get current window size: %s\n", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
			}
			newW, newH, err := ms.getWindowSize()
			if err != nil {
				ms.conf.Logger.Printf("failed to get new window size: %s\n", err)
				continue
			}
			if newW == w && newH == h {
				continue
			}
			_, err = ms.sess.SendRequest("window-change", false, ssh.Marshal(
				windowChangeReq{W: uint32(newW), H: uint32(newH)},
			))
			if err != nil {
				ms.conf.Logger.Printf("failed to set new window size: %s\n", err)
			} else {
				w = newW
				h = newH
			}
		}
	}()
}

func (ms *MinSSH) invokeInOutPipes() {
	go func() {
		err := ms.copyToStdout()
		if err != nil {
			ms.conf.Logger.Printf("failed to copy remote stdout to local one: %s\n", err)
		}
	}()

	go func() {
		err := ms.copyToStderr()
		if err != nil {
			ms.conf.Logger.Printf("failed to copy remote stderr to local one: %s\n", err)
		}
	}()

	go func() {
		buf := make([]byte, 128)
		for {
			n, err := ms.readFromStdin(buf)
			if err != nil {
				if err != io.EOF {
					ms.conf.Logger.Printf("failed to read bytes from local stdin: %s\n", err)
				}
				ms.rStdin.Close()
				return
			}
			if n > 0 {
				_, err := ms.rStdin.Write(buf[:n])
				if err != nil {
					ms.conf.Logger.Printf("failed to write bytes to remote stdin: %s\n", err)
					return
				}
			}
		}
	}()
}

func (ms *MinSSH) printExitMessage(err error) {
	fmt.Printf("ssh connection to %s closed ", ms.conf.Host)
	if err != nil {
		switch e := err.(type) {
		case *ssh.ExitMissingError:
			fmt.Printf("but remote didn't send exit status: %s\n", e)
		case *ssh.ExitError:
			fmt.Printf("with error: %s\n", e)
		default:
			fmt.Printf("with unknown error: %s\n", err)
		}
	} else {
		fmt.Println("successfully")
	}
}

func (ms *MinSSH) RunInteractive() error {
	if err := ms.prepareRemoteTerminal(); err != nil {
		return err
	}
	if err := ms.prepareLocalTerminal(); err != nil {
		return err
	}

	sigC := ms.watchSignals()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		signal.Stop(sigC)
		cancel()
		ms.wg.Wait()
	}()

	ms.invokeResizeTerminal(ctx)
	ms.invokeInOutPipes()

	sessC := make(chan error)
	go func() {
		sessC <- ms.sess.Wait()
	}()

	select {
	case <-sigC:
		fmt.Println("got signal")
	case err := <-sessC:
		ms.printExitMessage(err)
	}

	return nil
}
