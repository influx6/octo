package utils

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/influx6/octo"
	"github.com/influx6/octo/netutils"
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
			credential.Data = decodeSplit[2]
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
			credential.Data = bearSplit[1]
		}

		return credential, nil
	}

	if len(bearSplit) == 0 {
		return credential, errors.New("Invalid Credential, expect `Key:Token:[OPTIONAL DATA]`")
	}

	if len(bearSplit) == 3 {
		credential.Key = bearSplit[0]
		credential.Token = bearSplit[1]
		credential.Data = bearSplit[2]
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
