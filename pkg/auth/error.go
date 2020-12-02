package auth

import "errors"

var (
	// ErrEmptyChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	ErrEmptyChallenge = errors.New("Empty challenge header")
	// ErrInvalidChallenge indicates an issue with the received challenge in the WWW-Authenticate header
	ErrInvalidChallenge = errors.New("Invalid challenge header")
	// ErrNoNewChallenge indicates a challenge update did not result in any change
	ErrNoNewChallenge = errors.New("No new challenge")
	// ErrNotFound indicates no credentials found for basic auth
	ErrNotFound = errors.New("No credentials available for basic auth")
	// ErrNotImplemented returned when method has not been implemented yet
	ErrNotImplemented = errors.New("Not implemented")
	// ErrParseFailure indicates the WWW-Authenticate header could not be parsed
	ErrParseFailure = errors.New("Parse failure")
	// ErrUnauthorized request was not authorized
	ErrUnauthorized = errors.New("Unauthorized")
	// ErrUnsupported indicates the request was unsupported
	ErrUnsupported = errors.New("Unsupported")
)
