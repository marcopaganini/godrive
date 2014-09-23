package gdrive_path

// Custom error functions for gdrive_path
//
// This file is part of the gdrive_path library
//
// (C) Sep/2014 by Marco Paganini <paganini@paganini.net>

type GdrivePathError struct {
	ObjectNotFound bool
	msg            string
}

func (e *GdrivePathError) Error() string {
	return e.msg
}

func IsObjectNotFound(e error) bool {
	serr, ok := e.(*GdrivePathError)
	if ok && serr.ObjectNotFound {
		return true
	}
	return false
}
