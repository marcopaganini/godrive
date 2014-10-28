package gdrive_path

// This library requires the Google Drive SDK to run.
//
// For details, check the README.md file with this distribution.
//
// This file contains the "high level" methods of gdrive_path.  Most users will
// want to call methods inside this file.
//
// should be considered ALPHA quality for the time being. The author will not
// be help responsible if it eats all of your files, kicks your cat and runs
// away with you wife/husband.
//
// (C) Oct/2014 by Marco Paganini <paganini@paganini.net>

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"code.google.com/p/google-api-go-client/drive/v2"
)

// Download a file from Gdrive. Returns an io.Reader to gdrive file pointed by srcPath.
// The io.Reader can be used to save the file locally by the caller.
func (g *Gdrive) Download(srcPath string) (io.Reader, error) {
	// Sanitize
	_, _, srcPath = splitPath(srcPath)
	if srcPath == "" {
		return nil, fmt.Errorf("Download: empty source path")
	}

	srcFileObj, err := g.Stat(srcPath)
	if err != nil {
		return nil, err
	}
	if srcFileObj.DownloadUrl == "" {
		return nil, fmt.Errorf("Download: File \"%s\" is not downloadable (no body?)", srcPath)
	}

	req, err := http.NewRequest("GET", srcFileObj.DownloadUrl, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.transport.RoundTrip(req)
	return resp.Body, err
}

// Download a file named 'srcPath' into 'localFile'. localFile will be
// overwritten if it exists. The file is first downloaded into a temporary file
// and then atomically moved into the destination file. Returns the number of bytes
// downloaded.
func (g *Gdrive) DownloadToFile(srcPath string, localFile string) (int64, error) {
	// Sanitize
	_, _, srcPath = splitPath(srcPath)
	if srcPath == "" {
		return 0, fmt.Errorf("DownloadToFile: empty source path")
	}
	if localFile == "" {
		return 0, fmt.Errorf("DownloadToFile: empty local file")
	}
	// If the file exists, it must be a regular file
	fi, err := os.Stat(localFile)
	if err != nil {
		if os.IsExist(err) && !fi.Mode().IsRegular() {
			return 0, fmt.Errorf("Download: Local file \"%s\" exists and is not a regular file", localFile)
		}
	}

	srcFileObj, err := g.Stat(srcPath)
	if err != nil {
		return 0, err
	}
	if srcFileObj.DownloadUrl == "" {
		return 0, fmt.Errorf("Download: File \"%s\" is not downloadable (no body?)", srcPath)
	}

	// Create a temporary file and write to it, renaming at the end.
	tmpFile := fmt.Sprintf("temp-%d-%d", rand.Int31(), rand.Int31())
	tmpWriter, err := os.Create(tmpFile)
	if err != nil {
		return 0, err
	}
	defer tmpWriter.Close()
	defer os.Remove(tmpFile)

	reader, err := g.Download(srcPath)
	if err != nil {
		return 0, err
	}

	written, err := io.Copy(tmpWriter, reader)
	if err != nil {
		return 0, err
	}

	err = os.Rename(tmpFile, localFile)
	if err != nil {
		return 0, err
	}

	return written, nil
}

// Insert a file named 'dstPath' with the contents coming from 'reader'. The
// method calls the 'insert' method with the inplace option set to false,
// causing the file to be writen to a temporary location and then renamed to
// its final place. This method is safer (but slower) than the InsertInPlace
// method.
//
// Returns *drive.File pointing to the file in its final location.
func (g *Gdrive) Insert(dstPath string, reader io.Reader) (*drive.File, error) {
	return g.insert(dstPath, reader, false)
}

// Insert a file named 'dstPath' with the contents coming from reader. The
// method calls the 'insert' method with the inplace option set to true,
// causing the file to be written directly to its final destination. This
// is faster but (theoretically) less safe than using "Insert".
//
// Returns *drive.File: pointing to the file in its final location.
func (g *Gdrive) InsertInPlace(dstPath string, reader io.Reader) (*drive.File, error) {
	return g.insert(dstPath, reader, true)
}

// Insert a file named 'dstPath' with the contents coming from reader. If
// 'inplace' is set to false, this method first inserts the file under
// DRIVE_TMP_FOLDER and then moves it to its final location. If inplace is set
// to true, the the methdo removes the destination file if it exists and
// uploads directly (this saves time). DRIVE_TMP_FOLDER will be automatically
// created, if needed.
//
// Returns *drive.File: pointing to the file in its final location.
func (g *Gdrive) insert(dstPath string, reader io.Reader, inplace bool) (*drive.File, error) {
	var (
		outDir     string
		outFile    string
		outPath    string
		parent     *drive.File
		outFileObj *drive.File
		err        error
	)

	if inplace {
		outDir, outFile, dstPath = splitPath(dstPath)
		outPath = dstPath
		parent, err = g.Stat(outDir)
		if err != nil {
			return nil, fmt.Errorf("insert: Unable to stat destination directory: \"%s\": %v", outDir, err)
		}
	} else {
		// We upload to DRIVE_TMP_FOLDER so it must always exist
		parent, err = g.Mkdir(DRIVE_TMP_FOLDER)
		if err != nil {
			return nil, err
		}

		outFile = fmt.Sprintf("temp-%d-%d", rand.Int31(), rand.Int31())
		outPath = DRIVE_TMP_FOLDER + "/" + outFile
	}

	// Delete output object if it already exists (file or directory)
	outFileObj, err = g.Stat(outPath)
	if err != nil && !IsObjectNotFound(err) {
		return nil, err
	}
	if !IsObjectNotFound(err) {
		_, err = g.GdriveFilesTrash(outFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("insert: Error removing (existing) destination file \"%s\": %v", outPath, err)
		}
	}

	// Insert file
	outFileObj, err = g.GdriveFilesInsert(reader, outFile, parent.Id, "")
	if err != nil {
		return nil, fmt.Errorf("insert: Error inserting file \"%s\": %v", outPath, err)
	}

	// Move file to definitive location if needed
	if !inplace {
		outFileObj, err = g.Move(outPath, dstPath)
		if err != nil {
			return nil, err
		}
		outPath = dstPath
	}

	cacheAdd(g.filecache, outPath, outFileObj)
	return outFileObj, nil
}

// Returns a slice of *drive.File objects under 'drivePath' matching 'query'
// (in Google Drive query format.) If query is blank, it defaults to 'trashed =
// false'.
func (g *Gdrive) ListDir(drivePath string, query string) ([]*drive.File, error) {
	var ret []*drive.File

	driveDir, err := g.Stat(drivePath)
	if err != nil {
		return nil, err
	}

	if query == "" {
		query = "trashed = false"
	}
	children, err := g.GdriveChildrenList(driveDir.Id, query)
	if err != nil {
		return nil, fmt.Errorf("ListDir: Error retrieving ChildrenList for path \"%s\": %v", drivePath, err)
	}

	for _, child := range children {
		driveFile, err := g.GdriveFilesGet(child.Id)
		if err != nil {
			return nil, fmt.Errorf("ListDir: Error fetching file metadata for path \"%s\": %v", drivePath, err)
		}
		ret = append(ret, driveFile)
	}

	return ret, nil
}

// Creates the directory (folder) specified by drivePath. Returns the
// *drive.File pointing to the object. If the folder already exists, the
// *drive.File of the existing folder will be returned (this saves one Stat
// when creating directories.)
func (g *Gdrive) Mkdir(drivePath string) (*drive.File, error) {
	var parentId string

	// Sanitize
	pathname, dirname, drivePath := splitPath(drivePath)
	if drivePath == "" {
		return nil, fmt.Errorf("Mkdir: Attempting to create a blank directory")
	}

	// If the path already exists, returns a *drive.File pointing to it
	driveFile, err := g.Stat(drivePath)
	if err != nil && !IsObjectNotFound(err) {
		return nil, err
	}
	if !IsObjectNotFound(err) {
		return driveFile, err
	}

	// If no path, start at root
	if pathname == "" {
		parentId = "root"
	} else {
		driveFile, err = g.Stat(pathname)
		if err != nil {
			return nil, err
		}
		parentId = driveFile.Id
	}

	driveFile, err = g.GdriveFilesInsert(nil, dirname, parentId, MIMETYPE_FOLDER)
	if err != nil {
		return nil, err
	}
	cacheAdd(g.filecache, drivePath, driveFile)
	return driveFile, nil
}

// Rename/Move the object in 'srcPath' (file or directory) to 'dstPath' by
// calling patch to replace dstPath as the parent of 'srcPath'.  The paths are
// full paths (dir/dir/dir.../file).  Returns the *drive.File containing the
// destination object.
func (g *Gdrive) Move(srcPath string, dstPath string) (*drive.File, error) {
	// Sanitize Source & Destination
	srcDir, _, srcPath := splitPath(srcPath)
	dstDir, dstFile, dstPath := splitPath(dstPath)

	if srcPath == "" || dstPath == "" {
		return nil, fmt.Errorf("Move: Source and destination paths must be set")
	}

	// We need the source parentId, destination Id and object Id
	srcParentObj, err := g.Stat(srcDir)
	if err != nil {
		return nil, err
	}
	srcObj, err := g.Stat(srcPath)
	if err != nil {
		return nil, err
	}
	dstDirObj, err := g.Stat(dstDir)
	if err != nil {
		return nil, err
	}

	// Remove destination file if it exists
	dstFileObj, err := g.Stat(dstPath)
	if err != nil && !IsObjectNotFound(err) {
		return nil, err
	}
	if !IsObjectNotFound(err) {
		_, err = g.GdriveFilesTrash(dstFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("Move: Error removing destination file \"%s\": %v", dstPath, err)
		}
		cacheDel(g.filecache, dstPath)
	}

	// Set parents and change name if needed
	driveFile, err := g.GdriveFilesPatch(srcObj.Id, dstFile, "", []string{dstDirObj.Id}, []string{srcParentObj.Id})
	cacheDel(g.filecache, srcPath)
	if err != nil {
		return nil, fmt.Errorf("Move: Error moving temporary file \"%s\" to \"%s\": %v", srcPath, dstPath, err)
	}
	cacheAdd(g.filecache, dstPath, driveFile)
	return driveFile, nil
}

// Set the debug level for future uses of the log.Debug{ln,f} methods.
func (g *Gdrive) SetDebugLevel(n int) {
	g.log.SetDebugLevel(n)
}

// Set the verbose level for future uses of the log.Verbose{ln,f} methods.
func (g *Gdrive) SetVerboseLevel(n int) {
	g.log.SetVerboseLevel(n)
}

// Set the modification date of the file/directory specified by 'drivePath' to
// 'modifiedDate'. Returns *drive.File pointing to the modified file/dir.
func (g *Gdrive) SetModifiedDate(drivePath string, modifiedDate time.Time) (*drive.File, error) {

	driveFile, err := g.Stat(drivePath)
	if err != nil {
		return nil, err
	}

	// For some reason Gdrive requires the date to contain the nano information
	// and Format will return a date without nano information if it happens to
	// be zero. Add 1ns to make sure format will produce a date in the right format.
	modifiedDate = modifiedDate.Truncate(1 * time.Second)
	modifiedDate = modifiedDate.Add(1 * time.Nanosecond)
	rfcDate := modifiedDate.Format(time.RFC3339Nano)

	// Set Date
	driveFile, err = g.GdriveFilesPatch(driveFile.Id, "", rfcDate, nil, nil)
	if err != nil {
		return nil, err
	}
	cacheAdd(g.filecache, drivePath, driveFile)
	return driveFile, nil
}

// Returns the *drive.File object for the last element in 'drivePath'.  The
// path must be specified as a full path (similar to unix filesystem path.)
//
// Google Drive allows more than one object with the same name and Unix
// filesystems do not. Stat returns an error if a duplicate is found anywhere
// in the requested path (which will require human intervention, and should
// never happen if only this set of routines is used to create files under that
// path.) Stat returns an instance of GdrivePathError with ObjectNotFound set
// if the requested object cannot be found. Use g.IsObjecNotFound(err) to test
// for this condition.
//
// Returns *drive.File object of the object pointed by the full path.
func (g *Gdrive) Stat(drivePath string) (*drive.File, error) {
	var (
		children []*drive.ChildReference
		query    string
		err      error
		subdirs  []string
	)

	// Cached?
	driveFile := cacheGet(g.filecache, drivePath)
	if driveFile != nil {
		return driveFile.(*drive.File), nil
	}

	// Special case for "/" (root)
	if drivePath == "/" {
		return g.GdriveFilesGet("root")
	}

	// Sanitize
	dirs, filename, drivePath := splitPath(drivePath)
	if drivePath == "" {
		return nil, fmt.Errorf("Stat: Trying to stat blank path")
	}

	parent := "root"

	// We make sure that:
	// - Every element in our path exists
	// - Every element in our path is a directory
	// - No duplicates exist anywhere in the path
	//
	// Note: this is expensive for what it is :(

	if dirs != "/" {
		subdirs = strings.Split(dirs, "/")

		for idx := 0; idx < len(subdirs); idx++ {
			elem := subdirs[idx]
			ppath := strings.Join(subdirs[0:idx+1], "/")

			// If partial path cached, we set the parent to the id
			// of the cached object and keep traversing down the path.
			child := cacheGet(g.childcache, ppath)
			if child != nil {
				parent = child.(*drive.ChildReference).Id
			} else {
				// Test: No elements in our directory path are files
				query = fmt.Sprintf("title = '%s' and trashed = false and mimeType != '%s'", escapeQuotes(elem), MIMETYPE_FOLDER)
				children, err = g.GdriveChildrenList(parent, query)

				if err != nil {
					return nil, err
				}
				if len(children) != 0 {
					return nil, fmt.Errorf("Stat: Element \"%s\" in path \"%s\" is a file, not a directory", elem, drivePath)
				}

				// Test: One and only one directory
				query = fmt.Sprintf("title = '%s' and trashed = false and mimeType = '%s'", escapeQuotes(elem), MIMETYPE_FOLDER)
				children, err = g.GdriveChildrenList(parent, query)
				if err != nil {
					return nil, err
				}
				if len(children) == 0 {
					return nil, &GdrivePathError{
						ObjectNotFound: true,
						msg:            fmt.Sprintf("Stat: Missing directory named \"%s\" in path \"%s\"", elem, drivePath),
					}
				}
				if len(children) > 1 {
					return nil, fmt.Errorf("Stat: More than one directory named \"%s\" exists in path \"%s\"", elem, drivePath)
				}
				parent = children[0].Id
				cacheAdd(g.childcache, ppath, children[0])
			}
		}
	}

	// At this point, the entire path is good. We now check for 'filename'
	// (which is really the last element in our path). It coud be a file or
	// a directory, but duplicates are not supported.

	if filename != "" {
		query = fmt.Sprintf("title = '%s' and trashed = false", escapeQuotes(filename))
		children, err = g.GdriveChildrenList(parent, query)
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			return nil, &GdrivePathError{
				ObjectNotFound: true,
				msg:            fmt.Sprintf("Stat: Object \"%s\" not found", drivePath),
			}
		}
		if len(children) > 1 {
			return nil, fmt.Errorf("Stat: More than one file/directory named \"%s\" exists in path \"%s\"", filename, drivePath)
		}
		parent = children[0].Id
	}

	// Parent contains the id of the last element

	ret, err := g.GdriveFilesGet(parent)
	if err == nil {
		cacheAdd(g.filecache, drivePath, ret)
	}
	return ret, err
}
