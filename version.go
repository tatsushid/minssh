package main

// version string of the command
const Version string = "v0.1.0"

var (
	// git commit hash embedded by packaging script
	commitHash string

	// package build date embedded by packaging script
	buildDate  string
)

func version() (ver string) {
	ver = Version
	if commitHash != "" {
		ver += "-" + commitHash
	}
	if buildDate != "" {
		ver += " BuildDate: " + buildDate
	}
	return
}
