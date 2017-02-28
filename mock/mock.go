package mock

import "github.com/influx6/octo"

// CredentialPocket defines a struct which implements the octo.crendentials interface.
type CredentialPocket struct {
	credentials octo.AuthCredential
}

// NewCredentialPocket returns a new instance of CredentialPocket.
func NewCredentialPocket(cred octo.AuthCredential) CredentialPocket {
	return CredentialPocket{
		credentials: cred,
	}
}

// Credential returns the crendentials for the giving system.
func (m CredentialPocket) Credential() octo.AuthCredential {
	return m.credentials
}

//================================================================================
