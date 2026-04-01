package sysctl

import (
	"os"
	"runtime"
	"time"
)

// EnvContext holds dynamic environment information for system prompts.
type EnvContext struct {
	WorkDir  string
	Date     string
	Platform string
}

// DetectEnv gathers the current environment context.
func DetectEnv() EnvContext {
	wd, _ := os.Getwd()
	return EnvContext{
		WorkDir:  wd,
		Date:     time.Now().Format("2006-01-02"),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
}
