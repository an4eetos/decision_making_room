package port

import "context"

// Identity is who an OpenID Connect provider says signed in.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

// IdentityProvider is an OpenID Connect issuer, driven with the
// authorization-code flow and PKCE.
type IdentityProvider interface {
	// AuthCodeURL is where to send the browser. The PKCE challenge is derived
	// from verifier; the verifier itself never leaves the server.
	AuthCodeURL(ctx context.Context, state, nonce, verifier string) (string, error)
	// Exchange redeems the code and returns the verified ID token's identity.
	// The token's signature, issuer, audience, expiry and nonce are checked.
	Exchange(ctx context.Context, code, verifier, nonce string) (Identity, error)
}
