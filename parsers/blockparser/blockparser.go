package blockparser

import (
	"bytes"
	"errors"
	"regexp"

	"github.com/influx6/octo"
	"github.com/influx6/octo/parsers/byteutils"
)

var (
	ctrl              = "\r\n"
	lineBreak         = []byte("|")
	emptyString       = []byte("")
	ctrlLine          = []byte(ctrl)
	newLine           = []byte("\n")
	newCl             = []byte("\r")
	colon             = byte(':')
	colonSlice        = []byte(":")
	endBracket        = byte('}')
	beginBracket      = byte('{')
	endBracketSlice   = []byte("}")
	beginBracketSlice = []byte("{")
	endColonBracket   = []byte("}:")
	beginColonBracket = []byte(":{")
	endTrace          = []byte("End Trace")
	clipMatch         = regexp.MustCompile("[^A-Za-z0-9]+")
)

// Blocks defines a package level parser using the blockParser specification.
/* The block octo.Command specification is based on the idea of a simple text based
   protocol which follows specific rules about messages. These rules which are:

   1. All messages must end with a CRTL line ending `\r\n`.

   2. All octo.Command must begin with a  opening(`{`) backet and close with a closing(`}`) closing bracket. eg
	    `{A|U|Runner}`.

	 3. If we need to seperate messages within brackets that contain characters like '(',')','{','}' or just a part is
	    kept whole, then using the octo.Command exclude block '()' with a octo.Command block will make that happen. eg `{A|U|Runner|(U | F || JR (Read | UR))}\r\n`
			Where `(U | F || JR (Read | UR))` is preserved as a single block when parsed into component blocks.

	 4. Multiplex messages can be included together by combining messages wrapped by the brackets with a semicolon(`:`) in between. eg
	 		`{A|U|Runner}:{+SUBS|R|}\r\n`.
*/
var Blocks blockParser

// blockParser defines a struct for the blockParser.
type blockParser struct{}

// Unparse turns the provided item into a byte slice, it expects either a
// command or slice of commands.
func (b blockParser) Unparse(msg interface{}) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}

	switch item := msg.(type) {
	case octo.Command:
		return byteutils.JoinMessages(item), nil
	case []octo.Command:
		return byteutils.JoinMessages(item...), nil
	}

	return nil, errors.New("Invalid Data")
}

// Parse parses the data data coming in and produces a series of Messages
// based on a base pattern.
func (b blockParser) Parse(msg []byte) ([]octo.Command, error) {
	if len(msg) == 0 || msg == nil {
		return nil, errors.New("Empty Data Received")
	}

	var messages []octo.Command

	blocks, err := b.SplitMultiplex(msg)
	if err != nil {
		return nil, err
	}

	// fmt.Printf("DBLOCKS: %+q\n", blocks)

	for _, block := range blocks {
		if len(block) == 0 {
			continue
		}

		blockParts := b.SplitParts(block)

		var command []byte
		var data [][]byte

		command = blockParts[0]

		// fmt.Printf("CMD: %+q -> %t\n", command, clipMatch.Match(command))
		if clipMatch.Match(command) {
			return nil, errors.New("Invalid Command format")
		}

		if len(blockParts) > 1 {
			data = blockParts[1:len(blockParts)]
		}

		messages = append(messages, octo.Command{
			Name: command,
			Data: data,
		})
	}

	return messages, nil
}

// SplitParts splits the parts of a octo.Command block divided by a vertical bar(`|`)
// symbol. It spits out the parts into their seperate enitites.
func (blockParser) SplitParts(msg []byte) [][]byte {
	var dataBlocks [][]byte

	var block []byte

	msg = bytes.TrimPrefix(msg, beginBracketSlice)
	msg = bytes.TrimSuffix(msg, endBracketSlice)
	msgLen := len(msg)

	var excludedBlock bool
	var excludedDebt int

	for i := 0; i < msgLen; i++ {
		item := msg[i]

		if item == '(' {
			excludedBlock = true
			excludedDebt++
		}

		if excludedBlock && item == ')' {
			block = append(block, item)
			excludedDebt--

			if excludedDebt <= 0 {
				excludedBlock = false
			}

			continue
		}

		if excludedBlock {
			block = append(block, item)
			continue
		}

		if item == '|' {
			dataBlocks = append(dataBlocks, block)
			block = nil
			continue
		}

		block = append(block, item)
	}

	if len(block) != 0 {
		dataBlocks = append(dataBlocks, block)
	}

	return dataBlocks
}

// SplitMultiplex will split messages down into their separate parts by
// breaking patterns of octo.Command packs into their seperate parts.
// multiplex octo.Command: `{A|U|Runner}:{+SUBS|R|}\r\n` => []{[]byte("A|U|Runner}\r\n"), []byte("+SUBS|R|bucks\r\n")}.
func (blockParser) SplitMultiplex(msg []byte) ([][]byte, error) {
	var blocks [][]byte

	var block []byte
	var messageStart bool

	msgLen := len(msg)

	if msg[0] != beginBracket {
		return nil, errors.New("Expected octo.Command block/blocks should be enclosed in '{','}'")
	}

	i := -1

	{
	blockLoop:
		for {
			if msg == nil || len(msg) == 0 {
				break blockLoop
			}

			i++

			if i >= len(msg) {
				break blockLoop
			}

			item := msg[i]

			switch item {
			case ':':
				if i+1 >= msgLen {
					return nil, errors.New("blocks must follow {contents} rule")
				}

				piece := msg[i-1]
				var nxtPiece []byte

				if i < msgLen && i+2 < msgLen {
					nxtPiece = msg[i : i+2]
				}

				if -1 != bytes.Compare(nxtPiece, beginColonBracket) && piece == endBracket {
					blocks = append(blocks, block)
					block = nil
					messageStart = false
					continue
				}

				block = append(block, item)
				continue

			case '{':
				// fmt.Printf("BEGINSTART: %+q -> %+q : %+q \n", item, block, msg)
				if messageStart {
					block = append(block, item)
					continue
				}

				messageStart = true
				block = append(block, item)
				continue

			case '}':
				// fmt.Printf("ClOSESTART: %+q -> %+q : %+q \n", item, block, msg)

				if !messageStart {
					return nil, errors.New("Invalid Start of block")
				}

				// Are we at octo.Command end and do are we starting a new block?
				if i+1 >= msgLen {
					blocks = append(blocks, block)
					block = nil
					break
				}

				if msg[i+1] == ':' && i+2 >= msgLen {
					return nil, errors.New("Invalid new Block start")
				}

				// fmt.Printf("MSG: %+q -> %+q : %+q\n", msg, block, item)
				// fmt.Printf("MSGD: %+q -> %+q : %+q : %+q\n", msg, msg[i+1:i+2], msg[i+1:i+3], msg[i+3:])

				if msg[i+1] == ':' && msg[i+2] == '{' {
					block = append(block, item)
					blocks = append(blocks, block)
					block = nil
					messageStart = false
					continue
				}

				// We are probably facing a new section without
				if item == '}' && bytes.Equal(msg[i+1:i+3], ctrlLine) {
					block = append(block, item)
					blocks = append(blocks, block)
					block = nil
					messageStart = false

					msg = msg[i+3:]
					i = -1
					// fmt.Printf("MSGOD: %+q -> %+q : %+q \n", item, block, msg)

					if len(msg) == 0 {
						break blockLoop
					}

					continue blockLoop
				}

				ctrl := msg[i+1 : msgLen]
				if 0 == bytes.Compare(ctrl, ctrlLine) {
					if item == endBracket {
						block = append(block, item)
					}

					blocks = append(blocks, block)
					block = nil
					break blockLoop
				}

				block = append(block, item)

			default:

				ctrl := msg[i:msgLen]
				if 0 == bytes.Compare(ctrl, ctrlLine) {
					break blockLoop
				}

				block = append(block, item)
			}
		}

	}

	if len(block) > 1 {
		blocks = append(blocks, block)
	}

	return blocks, nil
}
