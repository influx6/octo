package utils

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/influx6/octo"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/byteutils"
	uuid "github.com/satori/go.uuid"
)

// AuthCredentialFromJSON returns the value of AuthCredential using json encoding.
func AuthCredentialFromJSON(m []byte) (octo.AuthCredential, error) {
	var b octo.AuthCredential

	if err := json.Unmarshal(m, &b); err != nil {
		return b, err
	}

	return b, nil
}

// AuthCredentialToJSON returns the value of AuthCredential using json encoding.
func AuthCredentialToJSON(ac octo.AuthCredential) ([]byte, error) {
	data, err := json.Marshal(&ac)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// NewContact returns a new instance of a octo.Contact object.
func NewContact(addr string) octo.Contact {
	addr = netutils.GetAddr(addr)

	return octo.Contact{
		Addr:   addr,
		Remote: addr,
		Local:  addr,
		SUUID:  uuid.NewV4().String(),
		UUID:   uuid.NewV4().String(),
	}
}

// ToCommand returns the a octo.Command from the giving byte slice if its a
// jsonified data of a octo.Command.
func ToCommand(data []byte) (octo.Command, error) {
	var cmd octo.Command

	if err := json.Unmarshal(data, &cmd); err != nil {
		return cmd, err
	}

	return cmd, nil
}

// ToCommands returns a slice of commands parsed from the provide byte slice.
func ToCommands(data []byte) ([]octo.Command, error) {
	var commands []octo.Command

	if err := json.Unmarshal(data, &commands); err != nil {
		var single octo.Command

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
func NewCommand(name string, data ...string) octo.Command {
	var cmd octo.Command
	cmd.Name = name
	cmd.Data = byteutils.StringsToBytes(data...)

	return cmd
}

// NewCommandMessage returns a new Command object with its equivalent json encoded
// byte slice.
func NewCommandMessage(name string, data ...string) ([]byte, octo.Command, error) {
	var cmd octo.Command
	cmd.Name = (name)
	cmd.Data = byteutils.StringsToBytes(data...)

	mdata, err := json.Marshal(cmd)
	if err != nil {
		return nil, cmd, err
	}

	return mdata, cmd, nil
}

// NewCommandByte returns a new Command object with its equivalent json encoded
// byte slice.
func NewCommandByte(name []byte, data ...[]byte) ([]byte, octo.Command, error) {
	var cmd octo.Command
	cmd.Name = string(name)
	cmd.Data = data

	mdata, err := json.Marshal(cmd)
	if err != nil {
		return nil, cmd, err
	}

	return mdata, cmd, nil
}

// ParseAuthorization returns the giving scheme and values of the provided authentication
// scheme.
func ParseAuthorization(authorizationValue string) (octo.AuthCredential, error) {
	var credential octo.AuthCredential

	authSplit := strings.SplitN(authorizationValue, " ", 2)
	if len(authSplit) != 2 {
		return credential, errors.New("Invalid Authorization Header")
	}

	credential.Scheme = authSplit[0]

	if strings.ToLower(credential.Scheme) == "basic" {
		decode, err := base64.StdEncoding.DecodeString(authSplit[0])
		if err != nil {
			return credential, err
		}

		decodeSplit := strings.Split(string(decode), ":")
		if len(decodeSplit) < 2 {
			return credential, errors.New("Invalid Credential for 'Basic', expect Basic Username:Password:[OPTIONAL DATA]")
		}

		if len(decodeSplit) > 1 {
			credential.Key = decodeSplit[0]
			credential.Token = decodeSplit[1]
		}

		if len(decodeSplit) > 2 {
			credential.Data = []byte(decodeSplit[2])
		}

		return credential, nil
	}

	bearSplit := strings.Split(authSplit[1], ":")

	if strings.ToLower(credential.Scheme) == "bearer" {
		if len(bearSplit) == 0 {
			return credential, errors.New("Invalid Credential for 'Bearer', expect Bear Token:[OPTIONAL DATA]")
		}

		if len(bearSplit) == 1 {
			credential.Key = bearSplit[0]
			credential.Token = bearSplit[0]
		}

		if len(bearSplit) == 2 {
			credential.Key = bearSplit[0]
			credential.Token = bearSplit[0]
			credential.Data = []byte(bearSplit[1])
		}

		return credential, nil
	}

	if len(bearSplit) == 0 {
		return credential, errors.New("Invalid Credential, expect `Key:Token:[OPTIONAL DATA]`")
	}

	if len(bearSplit) == 3 {
		credential.Key = bearSplit[0]
		credential.Token = bearSplit[1]
		credential.Data = []byte(bearSplit[2])
	}

	if len(bearSplit) == 2 {
		credential.Key = bearSplit[0]
		credential.Token = bearSplit[1]
	}

	if len(bearSplit) == 1 {
		credential.Key = bearSplit[0]
		credential.Token = bearSplit[0]
	}

	return credential, nil
}
