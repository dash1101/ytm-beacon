//go:build windows

package discord

import (
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// SearchDirs: on Windows Discord listens on named pipes, not files.
func SearchDirs() []string { return []string{`\\.\pipe\`} }

// Endpoints returns the pipe names to try. Windows cannot cheaply list pipes,
// so all ten candidates are returned; the ones that don't exist fail instantly.
func Endpoints() []string {
	eps := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		eps = append(eps, fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i))
	}
	return eps
}

func dial(endpoint string) (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(endpoint, &timeout)
}
