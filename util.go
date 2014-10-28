package gdrive_path

// Utility functions for gdrive_path
//
// This file is part of the gdrive_path library
//
// (C) Sep/2014 by Marco Paganini <paganini@paganini.net>

import (
	"strings"
	"time"

	drive "code.google.com/p/google-api-go-client/drive/v2"
	"code.google.com/p/google-api-go-client/googleapi"
)

// CreateDate returns the time.Time representation of the *drive.File object's creation date.
func CreateDate(driveFile *drive.File) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, driveFile.CreatedDate)
}

// IsDir returns true if the passed *drive.File object is a directory
func IsDir(driveFile *drive.File) bool {
	return (driveFile.MimeType == MIMETYPE_FOLDER)
}

// ModifiedDate returns the time.Time representation of the *drive.File
// object's modification date, rounded to the nearest second. Comparing dates
// with nanosecond information leads to rounding errors.
func ModifiedDate(driveFile *drive.File) (time.Time, error) {
	tt, err := time.Parse(time.RFC3339Nano, driveFile.ModifiedDate)
	if err != nil {
		return time.Time{}, err
	}
	return tt.Truncate(time.Second), nil
}

// Escape single quotes inside string with a backslash
func escapeQuotes(str string) string {
	var ret []string
	if strings.Index(str, "'") == -1 {
		return str
	}
	tokens := strings.Split(str, "'")
	for idx := 0; idx < len(tokens); idx++ {
		ret = append(ret, tokens[idx])
		if idx != len(tokens)-1 {
			ret = append(ret, "\\'")
		}
	}
	return strings.Join(ret, "")
}

// Execute a Gdrive Do() operation returning a *drive.ChildList and error from the
// original operation. Retry operation (with exponential fallback) if a 5xx
// is received from the other side.
func driveChildListOpRetry(fn func() (*drive.ChildList, error)) (*drive.ChildList, error) {
	var (
		err            error
		driveChildList *drive.ChildList
	)
	for try := 1; try <= NUM_TRIES; try++ {
		driveChildList, err = fn()
		if err != nil {
			// HTTP error?
			if derr, ok := err.(*googleapi.Error); ok {
				// 5xx?
				if derr.Code >= 500 || derr.Code <= 599 {
					//time.Sleep(time.Millisecond * (rand.Int31n(2000) + 1000*try))
					time.Sleep(time.Millisecond * time.Duration(1000*try))
					continue
				}
			}
			return nil, err
		}
		return driveChildList, err
	}
	return nil, err
}

// Execute a Gdrive Do() operation returning a *drive.File and error from the
// original operation. Retry operation (with exponential fallback) if a 5xx
// is received from the other side.
func driveFileOpRetry(fn func() (*drive.File, error)) (*drive.File, error) {
	var (
		err       error
		driveFile *drive.File
	)
	for try := 1; try <= NUM_TRIES; try++ {
		driveFile, err = fn()
		if err != nil {
			// HTTP error?
			if derr, ok := err.(*googleapi.Error); ok {
				// 5xx?
				if derr.Code >= 500 || derr.Code <= 599 {
					//time.Sleep(time.Millisecond * (rand.Int31n(2000) + 1000*try))
					time.Sleep(time.Millisecond * time.Duration(1000*try))
					continue
				}
			}
			return nil, err
		}
		return driveFile, err
	}
	return nil, err
}

// splitPath takes a Unix like pathname, splits it on its components, and
// remove empty elements and unnecessary leading and trailing slashes.
//
// Returns:
//   - string: directory
//   - string: filename
//   - string: completely reconstructed path.
func splitPath(pathName string) (string, string, string) {
	var ret []string

	for _, e := range strings.Split(pathName, "/") {
		if e != "" {
			ret = append(ret, e)
		}
	}
	if len(ret) == 0 {
		return "", "", ""
	}
	// filename.foo or /filename.foo. Filenames without path
	// are always assumed to start at root. Gdrive has no concept
	// of current working directory.
	if len(ret) == 1 {
		return "/", ret[0], "/" + ret[0]
	}
	return strings.Join(ret[0:len(ret)-1], "/"), ret[len(ret)-1], strings.Join(ret, "/")
}
