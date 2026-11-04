// Command mockdiscord pretends to be the Discord desktop client's IPC socket
// (Linux/macOS), so you can develop and test YTM Beacon without touching your
// real Discord. It prints every presence update it receives.
//
//	go run ./tools/mockdiscord -dir /tmp/mock
//	XDG_RUNTIME_DIR=/tmp/mock YTM_BEACON_CLIENT_ID=123456789012345678 go run ./cmd/ytm-beacon run
//
// (On macOS set TMPDIR instead of XDG_RUNTIME_DIR.)
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
)

func readFrame(r io.Reader) (uint32, []byte, error) {
	var h [8]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	buf := make([]byte, binary.LittleEndian.Uint32(h[4:]))
	_, err := io.ReadFull(r, buf)
	return binary.LittleEndian.Uint32(h[:4]), buf, err
}

func writeFrame(w io.Writer, op uint32, payload []byte) error {
	b := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(b, op)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(payload)))
	copy(b[8:], payload)
	_, err := w.Write(b)
	return err
}

func main() {
	dir := flag.String("dir", os.TempDir(), "folder to create discord-ipc-0 in")
	reject := flag.Bool("reject-id", false, "answer the handshake with \"Invalid Client ID\"")
	rejectNew := flag.Bool("reject-new-fields", false, "answer ERROR to activities that use status_display_type or small_image")
	flag.Parse()

	path := filepath.Join(*dir, "discord-ipc-0")
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("mock Discord listening on", path)

	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go serve(c, *reject, *rejectNew)
	}
}

func serve(c net.Conn, reject, rejectNew bool) {
	defer c.Close()
	_, payload, err := readFrame(c)
	if err != nil {
		return
	}
	fmt.Printf("[handshake] %s\n", payload)
	if reject {
		_ = writeFrame(c, 2, []byte(`{"code":4000,"message":"Invalid Client ID"}`))
		return
	}
	_ = writeFrame(c, 1, []byte(`{"cmd":"DISPATCH","evt":"READY","data":{"v":1,"user":{"username":"mockuser","global_name":"Mock User"}},"nonce":null}`))

	for {
		op, payload, err := readFrame(c)
		if err != nil {
			fmt.Println("[disconnected]")
			return
		}
		if op != 1 {
			continue
		}
		var req struct {
			Cmd   string `json:"cmd"`
			Nonce string `json:"nonce"`
			Args  struct {
				Activity map[string]any `json:"activity"`
			} `json:"args"`
		}
		_ = json.Unmarshal(payload, &req)
		if req.Args.Activity == nil {
			fmt.Println("[activity] CLEARED")
		} else {
			pretty, _ := json.Marshal(req.Args.Activity)
			fmt.Printf("[activity] %s\n", pretty)
		}
		reply := map[string]any{"cmd": req.Cmd, "evt": nil, "nonce": req.Nonce, "data": req.Args.Activity}
		if rejectNew && req.Args.Activity != nil {
			assets, _ := req.Args.Activity["assets"].(map[string]any)
			_, hasStatus := req.Args.Activity["status_display_type"]
			if _, hasSmall := assets["small_image"]; hasSmall || hasStatus {
				reply = map[string]any{"cmd": req.Cmd, "evt": "ERROR", "nonce": req.Nonce, "data": map[string]any{"code": 4000, "message": "Invalid Payload"}}
				fmt.Println("[activity] -> replied ERROR (simulating an older Discord)")
			}
		}
		b, _ := json.Marshal(reply)
		_ = writeFrame(c, 1, b)
	}
}
