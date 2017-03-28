package commando

import (
	"encoding/json"
	"fmt"
)

// CommandMessage message defines a message type which is based on a plain text
// format which defines the command name and data as a set of '|' seperated items.
type CommandMessage struct {
	Name string   `json:"name"`
	Data [][]byte `json:"data"`
}

// ToCommand returns the a CommandMessage from the giving byte slice if its a
// jsonified data of a CommandMessage.
func ToCommand(data []byte) (CommandMessage, error) {
	var cmd CommandMessage

	if err := json.Unmarshal(data, &cmd); err != nil {
		return cmd, err
	}

	return cmd, nil
}

// ToCommands returns a slice of commands parsed from the provide byte slice.
func ToCommands(data []byte) ([]CommandMessage, error) {
	var commands []CommandMessage

	if err := json.Unmarshal(data, &commands); err != nil {
		var single CommandMessage

		errx := json.Unmarshal(data, &single)
		if errx == nil {
			commands = append(commands, single)
			return commands, nil
		}

		return nil, fmt.Errorf("Failed to parse: %q : %q", err.Error(), errx.Error())
	}

	return commands, nil
}

// NewCommand returns a new command object with the provided name and data.
func NewCommand(name string, data ...string) CommandMessage {
	var cmd CommandMessage
	cmd.Name = name
	cmd.Data = StringsToBytes(data...)

	return cmd
}

// NewCommandMessage returns a new Command object with its equivalent json encoded
// byte slice.
func NewCommandMessage(name string, data ...string) ([]byte, CommandMessage, error) {
	var cmd CommandMessage
	cmd.Name = (name)
	cmd.Data = StringsToBytes(data...)

	mdata, err := json.Marshal(cmd)
	if err != nil {
		return nil, cmd, err
	}

	return mdata, cmd, nil
}

// NewCommandByte returns a new Command object with its equivalent json encoded
// byte slice.
func NewCommandByte(name []byte, data ...[]byte) ([]byte, CommandMessage, error) {
	var cmd CommandMessage
	cmd.Name = string(name)
	cmd.Data = data

	mdata, err := json.Marshal(cmd)
	if err != nil {
		return nil, cmd, err
	}

	return mdata, cmd, nil
}
