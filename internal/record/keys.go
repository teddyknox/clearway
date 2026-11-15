package record

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
)

// privatePrefix starts the text form of a private key, so that a private key
// is not taken for a public key.
const privatePrefix = "clearway-private-key:"

// FormatPublicKey returns the text form of a public key: 64 hexadecimal
// digits.
func FormatPublicKey(k ed25519.PublicKey) string { return hex.EncodeToString(k) }

// ParsePublicKey reads the text form of a public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, privatePrefix) {
		return nil, errors.New("this is a private key; give the public key")
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("a public key is 64 hexadecimal digits")
	}
	return ed25519.PublicKey(b), nil
}

// FormatPrivateKey returns the text form of a private key: a prefix and the
// 64 hexadecimal digits of its seed.
func FormatPrivateKey(k ed25519.PrivateKey) string {
	return privatePrefix + hex.EncodeToString(k.Seed())
}

// ParsePrivateKey reads the text form of a private key.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, privatePrefix)
	if !ok {
		return nil, errors.New("not a Clearway private key")
	}
	seed, err := hex.DecodeString(rest)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("a private key holds 64 hexadecimal digits")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
