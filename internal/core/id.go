// Package core contains small value types shared by AquaOS domains. It has no
// dependencies on services or infrastructure, which keeps the dependency graph
// directed toward the core model.
package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
)

// ID is an RFC 4122 UUID encoded as a string. Keeping the public representation
// textual makes it stable across JSON, MQTT, databases, and future cluster nodes.
type ID string

// NewID returns a cryptographically random RFC 4122 version 4 UUID.
func NewID() (ID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	buffer := make([]byte, 36)
	hex.Encode(buffer[0:8], value[0:4])
	buffer[8] = '-'
	hex.Encode(buffer[9:13], value[4:6])
	buffer[13] = '-'
	hex.Encode(buffer[14:18], value[6:8])
	buffer[18] = '-'
	hex.Encode(buffer[19:23], value[8:10])
	buffer[23] = '-'
	hex.Encode(buffer[24:36], value[10:16])
	return ID(buffer), nil
}

// Validate reports whether ID has the canonical RFC 4122 textual shape.
func (id ID) Validate() error {
	value := string(id)
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return errors.New("ID must be an RFC 4122 UUID")
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil {
		return errors.New("ID must be an RFC 4122 UUID")
	}
	return nil
}
