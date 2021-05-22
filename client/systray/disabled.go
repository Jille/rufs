// +build !windows,!darwin,!systray

package systray

func Run(_, _, _ func()) {
	select {}
}
