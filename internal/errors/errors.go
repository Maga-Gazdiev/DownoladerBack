package errors

import "errors"

var ErrBusy = errors.New("service capacity exhausted")

var ErrUnsupportedURL = errors.New("unsupported video URL")
var ErrAuthentication = errors.New("platform requires authentication cookies")

type PermanentError struct{ Err error }

func (e *PermanentError) Error() string {
	return e.Err.Error()
}
func (e *PermanentError) Unwrap() error {
	return e.Err
}
func Permanent(err error) error {
	return &PermanentError{Err: err}
}
func IsPermanent(err error) bool {
	var target *PermanentError
	return errors.As(err, &target)
}
