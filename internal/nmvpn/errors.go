package nmvpn

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// ErrorPrefix is NM_DBUS_VPN_ERROR_PREFIX.
const ErrorPrefix = "org.freedesktop.NetworkManager.VPN.Error"

// ErrorCode is the nick of an NMVpnPluginError value; the D-Bus error name is
// ErrorPrefix + "." + code.
type ErrorCode string

const (
	ErrFailed                  ErrorCode = "Failed"
	ErrStartingInProgress      ErrorCode = "StartingInProgress"
	ErrAlreadyStarted          ErrorCode = "AlreadyStarted"
	ErrStoppingInProgress      ErrorCode = "StoppingInProgress"
	ErrAlreadyStopped          ErrorCode = "AlreadyStopped"
	ErrWrongState              ErrorCode = "WrongState"
	ErrBadArguments            ErrorCode = "BadArguments"
	ErrLaunchFailed            ErrorCode = "LaunchFailed"
	ErrInvalidConnection       ErrorCode = "InvalidConnection"
	ErrInteractiveNotSupported ErrorCode = "InteractiveNotSupported"
)

// Error is a plugin error carrying an NMVpnPluginError code. Handlers return
// it (via Errorf) to control the D-Bus error name NetworkManager sees.
type Error struct {
	Code ErrorCode
	Err  error
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Errorf returns an *Error with the given code.
func Errorf(code ErrorCode, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// toDBusError maps any error to a D-Bus error; untyped errors become Failed.
func toDBusError(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	var de *dbus.Error
	if errors.As(err, &de) {
		return de
	}
	code, msg := ErrFailed, err.Error()
	var pe *Error
	if errors.As(err, &pe) {
		code, msg = pe.Code, pe.Err.Error()
	}
	return &dbus.Error{Name: ErrorPrefix + "." + string(code), Body: []any{msg}}
}

func newDBusError(code ErrorCode, format string, args ...any) *dbus.Error {
	return toDBusError(Errorf(code, format, args...))
}
