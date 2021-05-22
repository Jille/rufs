package version

import (
	"fmt"
	"strings"
)

var (
	gitTag    string
	gitCommit string
)

func GetVersion() string {
	if gitTag == "" {
		return "(development build)"
	}
	if strings.HasSuffix(gitTag, gitCommit[:7]) {
		return gitTag
	} else {
		return fmt.Sprintf("%s (%s)", gitTag, gitCommit[:7])
	}
}
