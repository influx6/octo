package commando_test

import (
	"testing"

	"github.com/influx6/faux/tests"
	"github.com/influx6/octo/messages/commando"
)

func TestBlockMessageParser(t *testing.T) {
	msgs, err := commando.Parser.Decode([]byte(`{A|U|Runner}:{+SUBS|R|}\r\n`))
	if err != nil {
		tests.Failed("Should have parsed message blocks: %s", err)
	}
	tests.Passed("Should have parsed message blocks: %#q", msgs)

	messages, _ := msgs.([]commando.CommandMessage)
	if len(messages) > 2 {
		tests.Failed("Should have parsed message block as 2 but go %d", len(messages))
	}
	tests.Passed("Should have parsed message block as 2")
}

func TestBlockMessageParserWithExcludedBlock(t *testing.T) {
	msgs, err := commando.Parser.Decode([]byte(`{A|U|Runner|(U | F || JR (Read | UR))}\r\n`))
	if err != nil {
		tests.Failed("Should have parsed message blocks: %s", err)
	}
	tests.Passed("Should have parsed message blocks: %#q", msgs)

	if messages, _ := msgs.([]commando.CommandMessage); len(messages) > 1 {
		tests.Failed("Should have parsed message block as one but go %d", len(messages))
	}
	tests.Passed("Should have parsed message block as one")
}

func TestParserBlocks(t *testing.T) {
	blocks, err := commando.Parser.SplitMultiplex([]byte(`{A|U|Runner}:{+SUBS|R|}\r\n`))
	if err != nil {
		tests.Failed("Should have parsed blocks: %s", err)
	}
	tests.Passed("Should have parsed blocks: %+s", blocks)

	firstBlock := commando.Parser.SplitParts(blocks[0])
	if len(firstBlock) != 3 {
		tests.Failed("Should have parsed block[%+s] into 3 parts: %+s", blocks[0], firstBlock)
	}

	tests.Passed("Should have parsed block[%+s] into 3 parts: %+s", blocks[0], firstBlock)
}

func TestBadBlock(t *testing.T) {
	block, err := commando.Parser.SplitMultiplex([]byte(`{A|D|"{udss\r\n}":`))
	if err != nil {
		tests.Passed("Should have failed to parse blocks: %+s", err)
		return
	}

	tests.Failed("Should have failed to parse blocks: %+s", block)
}

func TestBadParserBlocks(t *testing.T) {
	_, err := commando.Parser.SplitMultiplex([]byte(`{A|D|"{udss\r\n}"}:`))
	if err != nil {
		tests.Passed("Should have failed to parse blocks: %+s", err)
		return
	}

	tests.Failed("Should have failed to parse blocks")
}

func TestSimpleParserBlocks(t *testing.T) {
	blocks, err := commando.Parser.SplitMultiplex([]byte("{INFO}\r\n"))
	if err != nil {
		tests.Failed("Should have parsed blocks: %s", err)
	}

	tests.Passed("Should have parsed blocks: %+q", blocks)
}

func TestComplexParserBlocks(t *testing.T) {
	blocks, err := commando.Parser.SplitMultiplex([]byte(`{A|D|"{udss\n\r}"}:{+SUBS|R|}\r\n`))
	if err != nil {
		tests.Failed("Should have parsed blocks: %s", err)
	}

	tests.Passed("Should have parsed blocks: %+s", blocks)
}
