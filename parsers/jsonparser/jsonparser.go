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

// Unparse takes a giving value and transforms it into a giving byte slice.
// It expects a command or a slice of commands.
func (jsonparser) Unparse(msg interface{}) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}

	switch item := msg.(type) {
	case octo.Command:
		return json.Marshal([]octo.Command{item})
	case []octo.Command:
		return json.Marshal(item)
	}

	return nil, errors.New("Invalid Data")
}

// Parse attempts to use `encode/json` to parse giving byte into a slice of command
// objects.
func (jsonparser) Parse(msg []byte) ([]octo.Command, error) {
	if len(msg) == 0 || msg == nil {
		return nil, errors.New("Empty Data Received")
	}

	var commands []octo.Command

	if err := json.Unmarshal(msg, &commands); err != nil {
		var single octo.Command

		errx := json.Unmarshal(msg, &single)
		if errx == nil {
			commands = append(commands, single)
			return commands, nil
		}

		return nil, err
	}

	return commands, nil
}
