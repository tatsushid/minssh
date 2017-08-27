package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/bburgin/minssh/pkg/minssh"
)

var defaultKnownHostsFiles = []string{
	"known_hosts",
	"known_hosts2",
}

var defaultIdentityFiles = []string{
	"id_rsa",
	"id_dsa",
	"id_ecdsa",
	"id_ed25519",
}

func getValidOptions() (validOptions map[string]map[string]string) {
	validOptions = make(map[string]map[string]string)
	validOptions["StrictHostKeyChecking"] = map[string]string{
		"valids": "yes or no",
		"default": "yes",
	}
	validOptions["Password"] = map[string]string{
		"valids": "any string",
		"default": "no password",
	}
	return
}

func getOptionData() (
		validOptions map[string]map[string]string,
		validOptionKeys []string,
		optionsMsg string) {
	optionsMsg = "Use `option` to specify options for which there is no separate\n"
	optionsMsg += "	command-line flag. This can be called multiple times.\n"
	optionsMsg += "\tValid options:\n"
	validOptions = getValidOptions()
	validOptionKeys = make([]string, 0, len(validOptions))
	for key := range validOptions {
		validOptionKeys = append(validOptionKeys, key)
	}
	sort.Strings(validOptionKeys)
	for keyIndex := range validOptionKeys {
		key := validOptionKeys[keyIndex]
		optionsMsg += fmt.Sprintf("\t%s=%s, default is %s\n",
		key,
		validOptions[key]["valids"],
		validOptions[key]["default"])
	}
	return
}

func appendValidsMsg(valids string,
		option string,
		msgIn string) (msgOut string) {
	msgOut = msgIn
	msgOut += "   Valid values:\n"
	msgOut += "   " + valids + "\n"
	return
}

type strSliceValue []string

func (v *strSliceValue) Set(s string) error {
	*v = append(*v, s)
	return nil
}

func (v *strSliceValue) String() string {
	return "" // no default
}

func getAppName() (appName string) {
	appName = filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	return
}

type app struct {
	name    string
	flagSet *flag.FlagSet
	conf    *minssh.Config
	dir     string
	homeDir string
	logFile *os.File
}

func (a *app) initApp() (err error) {
	a.conf = minssh.NewConfig()

	if a.conf.Logger == nil {
		a.conf.Logger = log.New(ioutil.Discard, a.name+" ", log.LstdFlags)
	}

	dir := os.Getenv("HOME")
	a.homeDir = dir
	if dir == "" && runtime.GOOS == "windows" {
		dir = os.Getenv("APPDATA")
		a.homeDir = os.Getenv("USERPROFILE")
	}
	if runtime.GOOS == "windows" {
		a.dir = filepath.Join(dir, a.name)
	} else {
		a.dir = filepath.Join(dir, "."+a.name)
	}

	err = os.MkdirAll(a.dir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create an application directory: %s", err)
	}

	for i, f := range defaultKnownHostsFiles {
		f = filepath.Join(a.dir, f)
		if _, err := os.Lstat(f); err == nil {
			a.conf.KnownHostsFiles = append(a.conf.KnownHostsFiles, f)
		} else if os.IsNotExist(err) && i == 0 {
			// if there isn't "known_host" file, create a empty file
			if fh, err := os.OpenFile(f, os.O_RDONLY|os.O_CREATE, 0600); err == nil {
				fh.Close()
				a.conf.KnownHostsFiles = append(a.conf.KnownHostsFiles, f)
			}
		}
	}

	return
}

func (a *app) parseArgs() (err error) {
	var (
		options         []string
		logPath         string
		useOpenSSHFiles bool
		showVersion     bool
	)

	validOptions, validOptionKeys, optionsMsg := getOptionData()

	a.flagSet.Var((*strSliceValue)(&a.conf.IdentityFiles), "i", "use `identity_file` for public key authentication. this can be called multiple times")
	a.flagSet.Var((*strSliceValue)(&options), "o", optionsMsg)
	a.flagSet.IntVar(&a.conf.Port, "p", 22, "specify ssh server `port`")
	a.flagSet.BoolVar(&a.conf.QuietMode, "q", false, "Quiet mode. Suppresses most warning and diagnostic messages, default is false.")
	a.flagSet.BoolVar(&a.conf.IsSubsystem, "s", false, "treat command as subsystem")
	a.flagSet.StringVar(&logPath, "E", "", "specify `log_file` path. if it isn't set, it discards all log outputs")
	a.flagSet.BoolVar(&useOpenSSHFiles, "U", false, "use keys and known_hosts files in OpenSSH's '.ssh' directory")
	a.flagSet.BoolVar(&a.conf.NoTTY, "T", false, "disable pseudo-terminal allocation")
	a.flagSet.BoolVar(&showVersion, "V", false, "show version and exit")
	a.flagSet.Parse(os.Args[1:])

	if showVersion {
		fmt.Println(version())
		os.Exit(0)
	}

	if len(a.conf.IdentityFiles) == 0 {
		for _, f := range defaultIdentityFiles {
			f = filepath.Join(a.dir, f)
			if _, err := os.Lstat(f); err == nil {
				a.conf.IdentityFiles = append(a.conf.IdentityFiles)
			}
		}
		if useOpenSSHFiles {
			for _, f := range defaultIdentityFiles {
				f = filepath.Join(a.homeDir, ".ssh", f)
				if _, err := os.Lstat(f); err == nil {
					a.conf.IdentityFiles = append(a.conf.IdentityFiles, f)
				}
			}
		}
	}

	errorMsg := ""
	for _, option := range options {
		var rex = regexp.MustCompile("(\\w+)=(.*)")
		data := rex.FindAllStringSubmatch(option, -1)
		if len(data) > 0 {
			for _, keyVal := range data {
				key := keyVal[1]
				val := keyVal[2]
				switch key {
				case "StrictHostKeyChecking":
					switch val {
					case "yes":
						a.conf.StrictHostKeyChecking = true
					case "no":
						a.conf.StrictHostKeyChecking = false
					default:
						invalidValMsg := "Option %s has invalid value: %s\n"
						invalidValMsg = appendValidsMsg(
							validOptions[key]["valids"],
							key,
							invalidValMsg)
						errorMsg += fmt.Sprintf(invalidValMsg, key, val)
					}
				case "Password":
					a.conf.PromptUserForPassword = false
					a.conf.Password = val
				default:
					invalidOptionMsg := "Unknown option: %s\n"
					validsString := ""
					for keyIndex := range validOptionKeys {
						key := validOptionKeys[keyIndex]
						if keyIndex > 0 {
							validsString += ", "
						}
						validsString += key
					}
					invalidOptionMsg = appendValidsMsg(
						validsString,
						"options",
						invalidOptionMsg)
					errorMsg += fmt.Sprintf(invalidOptionMsg, key)
				}
			}
		} else {
			invalidSyntaxMsg := "Option %s has invalid syntax\n"
			invalidSyntaxMsg += "   Please specify an option as a key=value pair\n"
			errorMsg += fmt.Sprintf(invalidSyntaxMsg, option)
		}
	}
	if len(errorMsg) > 0 {
		return fmt.Errorf(errorMsg)
	}

	if useOpenSSHFiles {
		for _, f := range defaultKnownHostsFiles {
			f = filepath.Join(a.homeDir, ".ssh", f)
			if _, err := os.Lstat(f); err == nil {
				a.conf.KnownHostsFiles = append(a.conf.KnownHostsFiles, f)
			}
		}
	}

	if logPath != "" {
		a.logFile, err = os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open logfile: %s\n", err)
			fmt.Fprintln(os.Stderr, "will not log, just ignore it")
		} else {
			a.conf.Logger = log.New(a.logFile, a.name+" ", log.LstdFlags)
		}
	}

	userHost := a.flagSet.Arg(0)
	if userHost == "" {
		return fmt.Errorf("ssh server host must be specified")
	}

	if i := strings.Index(userHost, "@"); i != -1 {
		a.conf.User = userHost[:i]
		a.conf.Host = userHost[i+1:]
	} else {
		a.conf.Host = userHost
	}

	if a.flagSet.NArg() > 1 {
		a.conf.Command = strings.Join(a.flagSet.Args()[1:], " ")
	}

	return
}

func (a *app) run() (exitCode int) {
	exitCode = 1

	err := a.initApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	err = a.parseArgs()
	if a.logFile != nil {
		defer a.logFile.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	if a.conf.Command == "" && !a.conf.NoTTY {
		if ok, err := minssh.IsTerminal(); !ok {
			fmt.Fprintln(os.Stderr, err)
			return
		}
	}

	ms, err := minssh.Open(a.conf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer ms.Close()

	err = ms.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	return 0
}

func main() {
	appName := getAppName()
	a := &app{
		name:    appName,
		flagSet: flag.NewFlagSet(appName, flag.ExitOnError),
	}
	a.flagSet.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [user@]hostname\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		a.flagSet.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nVersion:\n  %s", version())
	}

	os.Exit(a.run())
}
