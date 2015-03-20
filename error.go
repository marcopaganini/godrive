package godrive

// Custom error functions for godrive
//
// This file is part of the godrive library
//
// (C) 2015 by Marco Paganini <paganini@paganini.net>

// Error defines a custom error for godrive
type Error struct {
	ObjectNotFound bool
	msg            string
}

func (e *Error) Error() string {
	return e.msg
}

// IsObjectNotFound Returns true if the passed error is of type godrive.Error
// and the error condition was caused by an Object Not Found.
func IsObjectNotFound(e error) bool {
	serr, ok := e.(*Error)
	if ok && serr.ObjectNotFound {
		return true
	}
	return false
}
