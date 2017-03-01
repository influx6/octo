package utils

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/influx6/octo"
	"github.com/influx6/octo/netutils"
	"github.com/influx6/octo/parsers/byteutils"
	uuid "github.com/satori/go.uuid"
)

// NewInfo returns a new instance of a Info object.
func NewInfo(addr string) octo.Info {
	addr = netutils.GetAddr(addr)

	return octo.Info{
		Addr:   addr,
		Remote: addr,
		Local:  addr,
		SUUID:  uuid.NewV4().String(),
		UUID:   uuid.NewV4().String(),
	}
}

// NewCommand returns a new command object with the provided name and data.
func NewCommand(name string, data ...string) octo.Command {
	var cmd octo.Command
	cmd.Name = []byte(name)
	cmd.Data = byteutils.StringsToBytes(data...)

	return cmd
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
