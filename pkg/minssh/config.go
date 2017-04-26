package minssh

import (
	"io/ioutil"
	"log"
	"os"
)

type Config struct {
	User            string
	Host            string
	Port            int
	Logger          *log.Logger
	KnownHostsFiles []string
	IdentityFiles   []string
	NoTTY           bool
}

func NewConfig() *Config {
	return &Config{
		User:   getDefaultUser(),
		Host:   "",
		Port:   22,
		Logger: log.New(ioutil.Discard, "minssh ", log.LstdFlags),
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
