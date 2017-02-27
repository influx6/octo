package byteutils

import (
	"bytes"

	"github.com/influx6/octo"
)

var (
	ctrl              = "\r\n"
	lineBreak         = []byte("|")
	emptyString       = []byte("")
	ctrlLine          = []byte(ctrl)
	colonSlice        = []byte(":")
	endBracket        = byte('}')
	beginBracket      = byte('{')
	endBracketSlice   = []byte("}")
	beginBracketSlice = []byte("{")
	endColonBracket   = []byte("}:")
)

// MakeMessage wraps each byte slice in the multiple byte slice with with
// given header returning a single byte slice joined with a colon : symbol.
func MakeMessage(header string, msgs ...string) []byte {
	var msg []byte

	if msgs != nil {
		var bmsg [][]byte

		for _, msg := range msgs {
			bmsg = append(bmsg, []byte(msg))
		}

		msg = WrapBlockParts(bmsg)
	}

	return WrapCTRLLine(WrapBlock(WrapWithHeader([]byte(header), msg)))
}

// MakeByteMessage wraps each byte slice in the multiple byte slice with with
// given header returning a single byte slice joined with a colon : symbol.
func MakeByteMessage(header []byte, msgs ...[]byte) []byte {
	var msg []byte

	if msgs != nil {
		msg = WrapBlockParts(msgs)
	}

	return WrapCTRLLine(WrapBlock(WrapWithHeader(header, msg)))
}

// MakeMessageGroup joins the provided message strings with a ':' character.
func MakeMessageGroup(msgs ...string) []byte {
	var bmsg [][]byte

	for _, msg := range msgs {
		bmsg = append(bmsg, []byte(msg))
	}

	return WrapCTRLLine(bytes.Join(bmsg, colonSlice))
}

// MakeByteMessageGroup joins series of messages with a ':' character.
func MakeByteMessageGroup(msg ...[]byte) []byte {
	return WrapCTRLLine(bytes.Join(msg, colonSlice))
}

// JoinMessages joins all the provided messages details returning a single byte
// slice.
func JoinMessages(mgs ...octo.Command) []byte {
	var bm [][]byte

	for _, msg := range mgs {
		bm = append(bm, WrapResponseBlock(msg.Name, msg.Data...))
	}

	return bytes.Join(bm, colonSlice)
}

// WrapCTRLLine wraps the giving message with a \r\n ending if not already there.
func WrapCTRLLine(msg []byte) []byte {
	if bytes.HasSuffix(msg, ctrlLine) {
		return msg
	}

	return append(msg, ctrlLine...)
}

// WrapBlock wraps a message in a opening and closing bracket.
func WrapBlock(msg []byte) []byte {
	if bytes.HasPrefix(msg, beginBracketSlice) && bytes.HasSuffix(msg, endBracketSlice) {
		return msg
	}

	return bytes.Join([][]byte{[]byte("{"), msg, []byte("}")}, emptyString)
}

// WrapBlockParts wraps the provided bytes slices with a | symbol.
func WrapBlockParts(msg [][]byte) []byte {
	if len(msg) == 0 {
		return nil
	}

	return bytes.Join(msg, lineBreak)
}

// WrapWithHeader wraps a message with a response wit the given header
// returning a single byte slice.
func WrapWithHeader(header []byte, msg []byte) []byte {
	if len(msg) == 0 {
		return header
	}

	return bytes.Join([][]byte{header, msg}, lineBreak)
}

// WrapResponses wraps each byte slice in the multiple byte slice with with
// given header returning a single byte slice joined with a colon : symbol.
func WrapResponses(header []byte, msgs ...[][]byte) []byte {
	var responses [][]byte

	for _, blocks := range msgs {
		var mod []byte

		if len(header) == 0 {
			mod = WrapWithHeader(header, WrapBlockParts(blocks))
		} else {
			mod = WrapBlockParts(blocks)
		}

		responses = append(
			responses,
			WrapBlock(mod),
		)
	}

	return bytes.Join(responses, colonSlice)
}

// WrapResponseBlock wraps each byte slice in the multiple byte slice with with
// given header returning a single byte slice joined with a colon : symbol.
func WrapResponseBlock(header []byte, msgs ...[]byte) []byte {
	var msg []byte

	if msgs != nil {
		msg = WrapBlockParts(msgs)
	}

	return WrapBlock(WrapWithHeader(header, msg))
}

// WrapResponse wraps each byte slice in the multiple byte slice with with
// given header returning a single byte slice joined with a colon : symbol.
func WrapResponse(header []byte, msgs ...[]byte) []byte {
	var responses [][]byte

	for _, block := range msgs {

		if header != nil {
			block = WrapWithHeader(header, block)
		}

		responses = append(
			responses,
			WrapBlock(block),
		)
	}

	return bytes.Join(responses, colonSlice)
}
