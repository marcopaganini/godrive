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
	"time"

	"code.google.com/p/google-api-go-client/drive/v2"
)

// Return an io.Reader to the file pointed by srcPath.
//
// Returns:
//	 - io.Reader
//   - error
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

// Downloads a file named 'srcPath' into 'localFile'. localFile will be
// overwritten if it exists. The file is first downloaded into a temporary file
// and then atomically moved into the destination file.
//
// Returns:
//	 - int64 - number of bytes downloaded
//   - error
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

// Insert a file named 'dstPath' with the contents coming from reader. The
// method calls the 'insert' method with the inplace option set to false,
// causing the file to be writen to a temporary location and then renamed to
// its final place. This method is safer (but slower) than the InsertInPlace
// method.
//
// Returns:
//   - *drive.File: pointing to the file in its final location.
//   - error
func (g *Gdrive) Insert(dstPath string, reader io.Reader) (*drive.File, error) {
	return g.insert(dstPath, reader, false)
}

// Insert a file named 'dstPath' with the contents coming from reader. The
// method calls the 'insert' method with the inplace option set to true,
// causing the file to be written directly to its final destination. This
// is faster but less safe than using "Insert".
//
// Returns:
//   - *drive.File: pointing to the file in its final location.
//   - error
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
// Returns:
//   - *drive.File: pointing to the file in its final location.
//   - error
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

// Insert a file named 'dstPath' with the contents of 'localFile'. This method
// first inserts the file under DRIVE_TMP_FOLDER and then moves it to its
// final location. DRIVE_TMP_FOLDER will be automatically created, if needed.
// The inserted object's modifiedDate will be set to the mtime of localFile.
//
// Returns:
//   - *drive.File: pointing to the file in its final location.
//   - error
func (g *Gdrive) InsertFile(dstPath string, localFile string) (*drive.File, error) {
	// Sanitize
	_, _, dstPath = splitPath(dstPath)
	if dstPath == "" {
		return nil, fmt.Errorf("InsertFile: empty destination path")
	}

	reader, err := os.Open(localFile)
	if err != nil {
		return nil, fmt.Errorf("InsertFile: Error opening \"%s\": %v", localFile, err)
	}
	_, err = g.Insert(dstPath, reader)
	if err != nil {
		return nil, err
	}

	// Set modified date to localFile's mtime
	fi, err := os.Stat(localFile)
	if err != nil {
		return nil, fmt.Errorf("InsertFile: Unable to stat localFile \"%s\": %v", localFile, err)
	}
	dstFileObj, err := g.SetModifiedDate(dstPath, fi.ModTime())
	if err != nil {
		return nil, fmt.Errorf("InsertFile: Unable to set date of \"%s\": %v", dstPath, err)
	}

	// No need to add to cache since Move (above) does it for us.
	return dstFileObj, nil
}

// ListDir returns a slice of *drive.File objects under 'drivePath'
// which match 'query'. If query is blank, it defaults to 'trashed = false'.
//
// Returns:
//   - []*drive.File of all objects inside drivePath matching query
//   - error
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

// Mkdir creates the directory (folder) specified by drivePath.
//
// Returns:
//   - *drive.File containing the object just created, or, *drive.File of
//     an existing object.
//   - err
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

// Rename/Move the object in 'srcPath' (file or directory) to 'dstPath'.  It
// does that by removing the directory in srcPath from the list of parents of
// the object, and adding dstPath.
//
// Returns:
//   - *drive.File containing the destination object
//   - error
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

// Set the debug level for future uses of the log.Debug{ln,f} methods
func (g *Gdrive) SetDebugLevel(n int) {
	g.log.SetDebugLevel(n)
}

// SetModifiedDate sets the modification date of the file/directory specified by
// 'drivePath' to 'modifiedDate'.
//
// Returns:
//   - *drive.File pointing to the modified file/dir
//   - error
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
