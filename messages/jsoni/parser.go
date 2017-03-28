package jsoni

import (
	"encoding/json"
	"errors"
)

// Parser exposes a global handler for accessing the jsonparser which implements the
// octo.Parser interface.
var Parser jsonparser

type jsonparser struct{}

// Encode takes a giving value and transforms it into a giving byte slice.
// It expects a command or a slice of commands.
func (jsonparser) Encode(msg interface{}) ([]byte, error) {
	if msg == nil {
		return nil, nil
	}

	switch item := msg.(type) {
	case AuthMessage:
		return json.Marshal(item)
	case CommandMessage:
		return json.Marshal([]CommandMessage{item})
	case []CommandMessage:
		return json.Marshal(item)
	}

	return nil, errors.New("Invalid Data")
}

// Decode attempts to use `encode/json` to parse giving byte into a slice of command
// objects.
func (jsonparser) Decode(msg []byte) (interface{}, error) {
	if len(msg) == 0 || msg == nil {
		return nil, errors.New("Empty Data Received")
	}

	var commands []CommandMessage

	if err := json.Unmarshal(msg, &commands); err != nil {
		var single CommandMessage

		errx := json.Unmarshal(msg, &single)
		if errx == nil {
			commands = append(commands, single)
			return commands, nil
		}

		return nil, err
	}

	return commands, nil
}
