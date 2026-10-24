package extbridge

import (
	"fmt"
	"io"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// readData 读取下一条文本消息。WebSocket 层的 ping 会通过 reply 回 pong。
// 调用方必须保证 reply 的写操作是串行的。
func readData(r io.Reader, reply func(op ws.OpCode, payload []byte) error, side ws.State, max int) ([]byte, error) {
	if max <= 0 {
		max = MaxMessageBytes
	}
	state := side
	var acc []byte
	for {
		header, err := ws.ReadHeader(r)
		if err != nil {
			return nil, err
		}
		if err := ws.CheckHeader(header, state); err != nil {
			return nil, err
		}
		if header.Length < 0 || header.Length > int64(max) || int64(len(acc))+header.Length > int64(max) {
			return nil, fmt.Errorf("%s: 消息超过 %d 字节", EProtocol, max)
		}
		payload := make([]byte, header.Length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		if header.Masked {
			ws.Cipher(payload, header.Mask, 0)
		}
		if header.OpCode.IsControl() {
			switch header.OpCode {
			case ws.OpPing:
				if reply != nil {
					if err := reply(ws.OpPong, payload); err != nil {
						return nil, err
					}
				}
			case ws.OpClose:
				return nil, io.EOF
			}
			continue
		}
		acc = append(acc, payload...)
		if header.Fin {
			return acc, nil
		}
		state = state.Set(ws.StateFragmented)
	}
}

// writeOp 写出一整帧。调用方负责串行。
func writeOp(w io.Writer, side ws.State, op ws.OpCode, payload []byte) error {
	return wsutil.WriteMessage(w, side, op, payload)
}
