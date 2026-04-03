package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Issuer creates and validates HMAC-SHA256 JWTs.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

// NewIssuer creates a JWT issuer with the given HMAC secret and token TTL.
func NewIssuer(secret string, ttl time.Duration) (*Issuer, error) {
	if secret == "" {
		return nil, errors.New("jwt: secret must not be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("jwt: ttl must be positive")
	}
	return &Issuer{secret: []byte(secret), ttl: ttl}, nil
}

// Claims represents the JWT payload.
type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	Gen  int64  `json:"gen"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

// header is the fixed JOSE header.
var header = mustB64JSON(map[string]string{"alg": "HS256", "typ": "JWT"})

// TTL returns the token time-to-live duration.
func (i *Issuer) TTL() time.Duration { return i.ttl }

// Create issues a new token for the given subject, role and token generation.
func (i *Issuer) Create(subject, role string, gen int64) (string, error) {
	if subject == "" {
		return "", errors.New("jwt: subject must not be empty")
	}
	now := time.Now().UTC()
	claims := Claims{
		Sub:  subject,
		Role: role,
		Gen:  gen,
		Iat:  now.Unix(),
		Exp:  now.Add(i.ttl).Unix(),
	}
	payload, err := b64JSON(claims)
	if err != nil {
		return "", fmt.Errorf("jwt: encode claims: %w", err)
	}
	unsigned := header + "." + payload
	sig := i.sign(unsigned)
	return unsigned + "." + sig, nil
}

// Parse validates a token and returns its claims.
func (i *Issuer) Parse(token string) (Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return Claims{}, errors.New("jwt: malformed token")
	}

	unsigned := parts[0] + "." + parts[1]
	expectedSig := i.sign(unsigned)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return Claims{}, errors.New("jwt: invalid signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("jwt: decode payload: %w", err)
	}

	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, fmt.Errorf("jwt: unmarshal claims: %w", err)
	}

	if time.Now().UTC().Unix() > c.Exp {
		return Claims{}, errors.New("jwt: token expired")
	}

	return c, nil
}

func (i *Issuer) sign(data string) string {
	h := hmac.New(sha256.New, i.secret)
	h.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func b64JSON(v interface{}) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func mustB64JSON(v interface{}) string {
	s, err := b64JSON(v)
	if err != nil {
		panic("jwt: " + err.Error())
	}
	return s
}
