package minssh

import (
	"io/ioutil"
	"log"
	"os"
)

type Config struct {
	User                  string
	PromptUserForPassword bool
	Password              string
	Host                  string
	Port                  int
	Logger                *log.Logger
	StrictHostKeyChecking bool
	KnownHostsFiles       []string
	IdentityFiles         []string
	Command               string
	QuietMode             bool
	IsSubsystem           bool
	NoTTY                 bool
}

func NewConfig() *Config {
	return &Config{
		User:   getDefaultUser(),
		Host:   "",
		Port:   22,
		Logger: log.New(ioutil.Discard, "minssh ", log.LstdFlags),
		PromptUserForPassword: true,
		StrictHostKeyChecking: true,
	}
}

func getDefaultUser() (username string) {
	for _, envKey := range []string{"LOGNAME", "USER", "LNAME", "USERNAME"} {
		username = os.Getenv(envKey)
		if username != "" {
			return
		}
	}

	return
}
