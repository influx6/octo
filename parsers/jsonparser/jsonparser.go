package jsonparser

import (
	"encoding/json"
	"errors"

	"github.com/influx6/octo"
)

// JSON exposes a global handler for accessing the jsonparser which implements the
// octo.Parser interface.
var JSON jsonparser

type jsonparser struct{}

// Parse attempts to use `encode/json` to parse giving byte into a slice of command
// objects.
func (jsonparser) Parse(msg []byte) ([]octo.Command, error) {
	if len(msg) == 0 {
		return nil, errors.New("Empty Data Received")
	}

	var commands []octo.Command

	if err := json.Unmarshal(msg, &commands); err != nil {
		return nil, err
	}

	return commands, nil
}
