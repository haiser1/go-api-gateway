package domain

// UpdateAPIKeyAuthRequest sets the machine API key. The raw secret is
// write-only and is stored hashed.
type UpdateAPIKeyAuthRequest struct {
	Secret string `json:"secret" validate:"required,min=16"`
	Header string `json:"header,omitempty"`
}

// UpdateHS256AuthRequest sets the HS256 JWT signing secret.
type UpdateHS256AuthRequest struct {
	Secret   string `json:"secret" validate:"required,min=32"`
	Header   string `json:"header,omitempty"`
	Issuer   string `json:"issuer,omitempty"`
	Audience string `json:"audience,omitempty"`
	KeyID    string `json:"kid,omitempty"`
	Leeway   string `json:"leeway,omitempty"`
}

// UpdateRS256AuthRequest sets the RS256 JWT public key (PEM or base64 PEM).
type UpdateRS256AuthRequest struct {
	PublicKey string `json:"public_key" validate:"required"`
	Header    string `json:"header,omitempty"`
	Issuer    string `json:"issuer,omitempty"`
	Audience  string `json:"audience,omitempty"`
	KeyID     string `json:"kid,omitempty"`
	Leeway    string `json:"leeway,omitempty"`
}
