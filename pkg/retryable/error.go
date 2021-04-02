package retryable

import "errors"

var (
	// ErrAllRequestsFailed when there are no mirrors left to try
	ErrAllRequestsFailed = errors.New("All requests failed")
	// ErrBackoffLimit maximum backoff attempts reached
	ErrBackoffLimit = errors.New("Backoff limit reached")
	// ErrCanceled if the context was canceled
	ErrCanceled = errors.New("Context was canceled")
	// ErrDigestMismatch if the expected digest wasn't received
	ErrDigestMismatch = errors.New("Digest mismatch")
	// ErrNotFound isn't there, search for your value elsewhere
	ErrNotFound = errors.New("Not found")
	// ErrNotImplemented returned when method has not been implemented yet
	ErrNotImplemented = errors.New("Not implemented")
	// ErrRetryNeeded indicates a request needs to be retried
	ErrRetryNeeded = errors.New("Retry needed")
	// ErrStatusCode indicates an unsuccessful status code
	ErrStatusCode = errors.New("Status code indicates the request failed")
	// ErrUnauthorized request was not authorized
	ErrUnauthorized = errors.New("Unauthorized")
)
