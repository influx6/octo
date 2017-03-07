package mock

import (
	"encoding/json"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/utils"
)

// CommandEncoding defines a struct for encoding and decoding octo.Command instructions
// from a tcp server.
type CommandEncoding struct{}

// Encode defines a function to encode the provided value which is expected to be
// a octo.Command struct which then will be encoding into json.
func (CommandEncoding) Encode(dl interface{}) ([]byte, error) {
	var err error
	var cmdData []byte

	switch item := dl.(type) {
	case octo.Command, *octo.Command:
		cmdData, err = json.Marshal(item)
	case []byte:
		return item, nil
	default:
		return nil, consts.ErrUnsupported
	}

	return cmdData, err
}

// Decode decodes the giving byte slice into a octo.Command struct using the json
// decoder.
func (CommandEncoding) Decode(dl []byte) (interface{}, error) {
	return utils.ToCommand(dl)
}
