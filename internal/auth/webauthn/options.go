package webauthn

// Ceremony options handed to the browser's navigator.credentials. Binary
// fields (challenge, ids) are base64url strings the frontend converts to
// ArrayBuffers; this keeps the API plain JSON.

type credParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

type credDescriptor struct {
	Type string `json:"type"`
	ID   string `json:"id"` // base64url
}

type rpEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type userEntity struct {
	ID          string `json:"id"` // base64url
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// CreationOptions is PublicKeyCredentialCreationOptions.
type CreationOptions struct {
	Challenge              string           `json:"challenge"`
	RP                     rpEntity         `json:"rp"`
	User                   userEntity       `json:"user"`
	PubKeyCredParams       []credParam      `json:"pubKeyCredParams"`
	Timeout                int              `json:"timeout"`
	AuthenticatorSelection map[string]any   `json:"authenticatorSelection"`
	ExcludeCredentials     []credDescriptor `json:"excludeCredentials,omitempty"`
	Attestation            string           `json:"attestation"`
}

// RequestOptions is PublicKeyCredentialRequestOptions.
type RequestOptions struct {
	Challenge        string           `json:"challenge"`
	Timeout          int              `json:"timeout"`
	RPID             string           `json:"rpId"`
	AllowCredentials []credDescriptor `json:"allowCredentials,omitempty"`
	UserVerification string           `json:"userVerification"`
}

// RegistrationOptions builds the create() options for a user.
func (v *Verifier) RegistrationOptions(userID []byte, userName, displayName string, challenge []byte, exclude [][]byte) *CreationOptions {
	return &CreationOptions{
		Challenge: b64.EncodeToString(challenge),
		RP:        rpEntity{ID: v.cfg.RPID, Name: v.cfg.RPName},
		User:      userEntity{ID: b64.EncodeToString(userID), Name: userName, DisplayName: displayName},
		PubKeyCredParams: []credParam{
			{Type: "public-key", Alg: algES256},
			{Type: "public-key", Alg: algRS256},
		},
		Timeout: 60000,
		// A platform or roaming authenticator both fine; user verification
		// preferred but not required (matches the assertion check on flagUP).
		AuthenticatorSelection: map[string]any{"userVerification": "preferred", "residentKey": "preferred"},
		ExcludeCredentials:     descriptors(exclude),
		Attestation:            "none",
	}
}

// AssertionOptions builds the get() options for a login.
func (v *Verifier) AssertionOptions(challenge []byte, allow [][]byte) *RequestOptions {
	return &RequestOptions{
		Challenge:        b64.EncodeToString(challenge),
		Timeout:          60000,
		RPID:             v.cfg.RPID,
		AllowCredentials: descriptors(allow),
		UserVerification: "preferred",
	}
}

func descriptors(ids [][]byte) []credDescriptor {
	out := make([]credDescriptor, 0, len(ids))
	for _, id := range ids {
		out = append(out, credDescriptor{Type: "public-key", ID: b64.EncodeToString(id)})
	}
	return out
}
