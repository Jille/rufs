package version

import "fmt"

var (
	gitTag    string
	gitCommit string
)

func GetVersion() string {
	if gitTag == "" {
		return "(development version)"
	}
	return fmt.Sprintf("%s (%s)", gitTag, gitCommit[:7])
}
