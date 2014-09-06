package gdrive_path

// This library requires the Google Drive SDK to run.
//
// For details, check the README.md file with this distribution.
//
// This library is under constant and rapid development but
// should be considered ALPHA quality for the time being. The
// author will not be help responsible if it eats all of your
// files, kicks your cat and runs away with you wife/husband.
//
// (C) Aug/2014 by Marco Paganini <paganini@paganini.net>

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	oauth "code.google.com/p/goauth2/oauth"
	drive "code.google.com/p/google-api-go-client/drive/v2"
)

// Mime-Type used by Google Drive to indicate a folder
const MIMETYPE_FOLDER = "application/vnd.google-apps.folder"

// Directory in Google Drive to hold temporary copies of files during inserts
const DRIVE_TMP_FOLDER = "tmp"

// Default cache TTL
const CACHE_TTL_SECONDS = 60

// Object cache
type objCache struct {
	driveFile drive.File
	timestamp time.Time
}

// Main Gdrive struct
type Gdrive struct {
	clientId     string
	clientSecret string
	code         string
	scope        string
	cacheFile    string

	transport *oauth.Transport
	client    *http.Client
	service   *drive.Service

	// Unique Id for this instance
	gdrive_uid int

	// Object cache
	cache map[string]*objCache
}

//------------------------------------------------------------------------------
//	Custom error object and methods
//------------------------------------------------------------------------------

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

// NewGdrivePath creates and returns a new *Gdrive Object.
func NewGdrivePath(clientId string, clientSecret string, code string, scope string, cacheFile string) (*Gdrive, error) {
	if clientId == "" || clientSecret == "" {
		return nil, fmt.Errorf("NewGdrivePath: Need both clientId and clientSecret")
	}

	g := &Gdrive{clientId: clientId, clientSecret: clientSecret, code: code, scope: scope, cacheFile: cacheFile}
	err := g.authenticate()
	if err != nil {
		return nil, err
	}
	g.client = g.transport.Client()
	g.service, err = drive.New(g.client)

	// Unique Id for this instance
	rand.Seed(time.Now().UnixNano())
	g.gdrive_uid = rand.Int()

	// Initialize blank cache
	g.cache = map[string]*objCache{}

	return g, err
}

//------------------------------------------------------------------------------
//	Private methods
//------------------------------------------------------------------------------

// cacheAdd: Add/replace object in the cache using 'drivePath' as a key
// Returns: nothing
func (g *Gdrive) cacheAdd(drivePath string, driveFile *drive.File) {
	obj := &objCache{*driveFile, time.Now()}
	g.cache[drivePath] = obj
}

// cacheGet: Retrieves object from the cache using 'drivePath' as a key
// Returns: *driveFile object or nil if not found or expired
func (g *Gdrive) cacheGet(drivePath string) *drive.File {
	obj, ok := g.cache[drivePath]
	if ok {
		if time.Now().After(obj.timestamp.Add(CACHE_TTL_SECONDS * time.Second)) {
			g.cacheDel(drivePath)
			return nil
		} else {
			return &obj.driveFile
		}
	}

	return nil
}

// cacheDel: Removes object from the cache using 'drivePath' as a key.
// Returns: nothing
func (g *Gdrive) cacheDel(drivePath string) {
	delete(g.cache, drivePath)
}

// Authenticates a newly created method (called by NewGdrivePath)
//
// This method authenticates the newly created object using clientId, clientSecret and code.
// cacheFile is used to store code and only needs to be specified once.
//
// Returns:
//   error: If the authentication process requires the user to fetch a new code, this method
//   returns error set with a message containing the URL to be used to fetch a new auth code.
func (g *Gdrive) authenticate() error {
	// Set up configuration
	config := &oauth.Config{
		ClientId:     g.clientId,
		ClientSecret: g.clientSecret,
		Scope:        g.scope,
		RedirectURL:  "oob",
		AuthURL:      "https://accounts.google.com/o/oauth2/auth",
		TokenURL:     "https://accounts.google.com/o/oauth2/token",
		TokenCache:   oauth.CacheFile(g.cacheFile),
	}

	// Set up a Transport using the config.
	g.transport = &oauth.Transport{Config: config}

	// Try to pull the token from the cache; if this fails, we need to get one.
	token, err := config.TokenCache.Token()
	if err != nil {
		if g.code == "" {
			// Get an authorization code from the data provider.
			// ("Please ask the user if I can access this resource.")
			url := config.AuthCodeURL("")
			return fmt.Errorf("authenticate: Code missing. To get a new one visit the url below:\n%s", url)
		}
		// Exchange the authorization code for an access token.
		// ("Here's the code you gave the user, now give me a token!")
		// If everything works, the Exchange method will cache the token.
		token, err = g.transport.Exchange(g.code)
		if err != nil {
			return fmt.Errorf("authenticate: Error exchanging code for token: %v", err)
		}
	}

	g.transport.Token = token
	return nil
}

//------------------------------------------------------------------------------
//	Static functions
//------------------------------------------------------------------------------

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
	if len(ret) == 1 {
		return "", ret[0], ret[0]
	}
	return strings.Join(ret[0:len(ret)-1], "/"), ret[len(ret)-1], strings.Join(ret, "/")
}

//------------------------------------------------------------------------------
//	Gdrive Primitives: Direct interfaces with Gdrive
//------------------------------------------------------------------------------

// GdriveFilesGet Returns a *drive.File object for the object identified by 'fileId'
func (g *Gdrive) GdriveFilesGet(fileId string) (*drive.File, error) {
	f, err := g.service.Files.Get(fileId).Do()
	if err != nil {
		return nil, fmt.Errorf("GdriveFilesGet: Error retrieving File Metadata for fileId \"%s\": %v", fileId, err)
	}
	return f, nil
}

// GdriveChildrenList Returns a slice of *drive.ChilReference containing all
// objects under 'ParentId' which satisfy the 'query' parameter.
func (g *Gdrive) GdriveChildrenList(parentId string, query string) ([]*drive.ChildReference, error) {
	var ret []*drive.ChildReference

	pageToken := ""
	for {
		c := g.service.Children.List(parentId)
		c.Q(query)
		if pageToken != "" {
			c = c.PageToken(pageToken)
		}
		r, err := c.Do()
		if err != nil {
			return nil, fmt.Errorf("GdriveChildrenList: fetching Id for parent_id \"%s\", query=\"%s\": %v", parentId, query, err)
		}
		ret = append(ret, r.Items...)
		pageToken = r.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return ret, nil
}

// GdriveFilesInsert inserts a new Object (file/dir) on Google Drive under
// 'parentId'. The object's contents will come from 'reader' (io.Reader). If
// reader is nil, an empty object will be created (this is how we create
// directories). The title of the object will be set to 'title' and the
// object's MIME Type will be set to 'mimeType', or automatically detected if
// mimeType is blank.
//
// This method returns a *drive.File object pointing to the file just inserted.
func (g *Gdrive) GdriveFilesInsert(reader io.Reader, title string, parentId string, mimeType string) (*drive.File, error) {
	var (
		err       error
		driveFile *drive.File
		ret       *drive.File
	)

	driveFile = &drive.File{Title: title, MimeType: mimeType}
	if mimeType != "" {
		driveFile.MimeType = mimeType
	}
	// Set parentId
	if parentId != "" {
		p := &drive.ParentReference{Id: parentId}
		driveFile.Parents = []*drive.ParentReference{p}
	}
	if reader != nil {
		ret, err = g.service.Files.Insert(driveFile).Media(reader).Do()
	} else {
		ret, err = g.service.Files.Insert(driveFile).Do()
	}
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// GdriveFilesPatch patches the file's metadata. The following information about the file
// can be changed:
//
// - Title
// - modifiedDate
// - addParentIds
// - removeParentIds
//
// Setting values to a blank string (when of type string) or nil will cause that
// particular attribute to remain untouched.
//
// Returns a *drive.File object pointing to the modified file.
func (g *Gdrive) GdriveFilesPatch(fileId string, title string, modifiedDate string, addParentIds []string, removeParentIds []string) (*drive.File, error) {
	driveFile := &drive.File{}
	if title != "" {
		driveFile.Title = title
	}
	if modifiedDate != "" {
		driveFile.ModifiedDate = modifiedDate
	}
	p := g.service.Files.Patch(fileId, driveFile)
	if len(addParentIds) > 0 {
		p.AddParents(strings.Join(addParentIds, ","))
	}
	if len(removeParentIds) > 0 {
		p.RemoveParents(strings.Join(removeParentIds, ","))
	}
	if modifiedDate != "" {
		p.SetModifiedDate(true)
	}
	r, err := p.Do()
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GdriveFilesTrash moves the file indicated by 'fileId' to the Google Drive Trash.
// It returns a *drive.File object pointing to the file inside Trash.
func (g *Gdrive) GdriveFilesTrash(fileId string) (*drive.File, error) {
	return g.service.Files.Trash(fileId).Do()
}

//------------------------------------------------------------------------------
//	Core user methods: These are the core of the library and most users of this
//	library will want to use them. Use the primitive calls sparingly and
//	carefully since they do not add/remove objects from the object cache.
//------------------------------------------------------------------------------

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

// Insert a file named 'dstPath' with the contents coming from reader. This
// method first inserts the file under DRIVE_TMP_FOLDER and then moves it to
// its final location. DRIVE_TMP_FOLDER will be automatically created, if
// needed.
//
// Returns:
//   - *drive.File: pointing to the file in its final location.
//   - error
func (g *Gdrive) Insert(dstPath string, reader io.Reader) (*drive.File, error) {
	// We always upload to DRIVE_TMP_FOLDER so it must always exist
	tmpDirObj, err := g.Mkdir(DRIVE_TMP_FOLDER)
	if err != nil {
		return nil, err
	}

	tmpFile := fmt.Sprintf("temp-%d-%d", rand.Int31(), rand.Int31())
	tmpPath := DRIVE_TMP_FOLDER + "/" + tmpFile

	// Delete temp file if it already exists (file or directory)
	tmpFileObj, err := g.Stat(tmpPath)
	if err != nil && !IsObjectNotFound(err) {
		return nil, err
	}
	if !IsObjectNotFound(err) {
		_, err = g.GdriveFilesTrash(tmpFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("Insert: Error removing existing temporary file \"%s\": %v", tmpPath, err)
		}
	}

	// Insert file into tmp dir with the temporary name
	tmpFileObj, err = g.GdriveFilesInsert(reader, tmpFile, tmpDirObj.Id, "")
	if err != nil {
		return nil, fmt.Errorf("Insert: Error inserting temporary file \"%s\": %v", tmpPath)
	}

	// Move file to definitive location
	dstFileObj, err := g.Move(tmpPath, dstPath)
	if err != nil {
		return nil, err
	}

	// No need to add to cache since Move (above) does it for us.
	return dstFileObj, nil
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
		return nil, fmt.Errorf("Insert: Unable to stat localFile \"%s\": %v", localFile, err)
	}
	dstFileObj, err := g.SetModifiedDate(dstPath, fi.ModTime())
	if err != nil {
		return nil, fmt.Errorf("Insert: Unable to set date of \"%s\": %v", dstPath, err)
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
	g.cacheAdd(drivePath, driveFile)
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
		g.cacheDel(dstPath)
	}

	// Set parents and change name if needed
	driveFile, err := g.GdriveFilesPatch(srcObj.Id, dstFile, "", []string{dstDirObj.Id}, []string{srcParentObj.Id})
	g.cacheDel(srcPath)
	if err != nil {
		return nil, fmt.Errorf("Move: Error moving temporary file \"%s\" to \"%s\": %v", srcPath, dstPath, err)
	}
	g.cacheAdd(dstPath, driveFile)
	return driveFile, nil
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
	g.cacheAdd(drivePath, driveFile)
	return driveFile, nil
}

// Stat receives a path like filename and parses each element in turn, returning
// the *drive.File object for the last element in the path.
//
// Google Drive allows more than one object with the same name and Unix
// filesystems do not. Stat returns an error if a duplicate is found anywhere
// in the requested path (which will require human intervention.) Stat returns
// an instance of GdrivePathError with ObjectNotFound set if the requested
// object cannot be found. Use g.IsObjecNotFound(err) to test for this
// condition.
//
// Returns:
//   - *drive.File object
//   - error
func (g *Gdrive) Stat(drivePath string) (*drive.File, error) {
	var (
		children []*drive.ChildReference
		query    string
		err      error
	)

	// Cached?
	driveFile := g.cacheGet(drivePath)
	if driveFile != nil {
		return driveFile, nil
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

	for _, elem := range strings.Split(dirs, "/") {
		// Split on blank strings returns one element
		if elem == "" {
			continue
		}

		// Test: No elements in our directory path are files
		query = fmt.Sprintf("title = '%s' and trashed = false and mimeType != '%s'", elem, MIMETYPE_FOLDER)
		children, err = g.GdriveChildrenList(parent, query)
		if err != nil {
			return nil, err
		}
		if len(children) != 0 {
			return nil, fmt.Errorf("Stat: Element \"%s\" in path \"%s\" is a file, not a directory", elem, drivePath)
		}

		// Test: One and only one directory
		query = fmt.Sprintf("title = '%s' and trashed = false and mimeType = '%s'", elem, MIMETYPE_FOLDER)
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
	}

	// At this point, the entire path is good. We now check for 'filename'
	// (which is really the last element in our path). It coud be a file or
	// a directory, but duplicates are not supported.

	if filename != "" {
		query = fmt.Sprintf("title = '%s' and trashed = false", filename)
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
		g.cacheAdd(drivePath, ret)
	}
	return ret, err
}
