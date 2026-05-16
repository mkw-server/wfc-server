package qr2

import (
	"bytes"

	"wwfc/logging"
)

func prefixIdx(buf []byte) int {
	return bytes.Index(buf, []byte{0xbb, 0xef, 0xdc, 0xc8})
}

// buffer must start with 0xbb, 0xef, 0xdc, 0xc8 or be empty
func verifyPrefix(buf []byte) bool {
	if len(buf) == 0 || (len(buf) >= 4 && prefixIdx(buf) == 0) {
		return true
	}

	logging.Info(moduleName, "Prefix is invalid due to nil buffer")
	return false
}

func addMsgToBuffer(addr string, msg []byte) *[]byte {
	buffer := mkwServerMessageBuffer[addr]
	if buffer == nil {
		buffer = &[]byte{}
		mkwServerMessageBuffer[addr] = buffer
	}
	if len(*buffer)+len(msg) > 0x500 {
		logging.Error(moduleName, addr, "sent a message that would overflow the buffer!")
		delete(mkwServerMessageBuffer, addr)
		return nil
	}
	combined := append(*buffer, msg...)
	if !verifyPrefix(combined) {
		logging.Error(moduleName, addr, "sent an invalid prefix!")
		delete(mkwServerMessageBuffer, addr)
		return nil
	}
	*buffer = combined
	return buffer
}

func popMessage(buf *[]byte) []byte {
	bufContents := *buf
	// returns 0 if the message starts with the prefix, needed in part to be complete
	if prefixIdx(bufContents) != 0 {
		return nil
	}
	// its not complete if the suffix can't be found
	suffixIdx := bytes.Index(bufContents, []byte{0xce, 0xf9, 0xd3, 0xaa})
	if suffixIdx == -1 {
		return nil
	}
	// found a message that can be handled. pop it, and update buffer to the next message
	*buf = (*buf)[suffixIdx+4:]
	return bufContents[4:suffixIdx]
}
