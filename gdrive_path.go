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
// (C) 2014 by Marco Paganini <paganini@paganini.net>

import (
	"fmt"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"path"
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
//	Non-exported methods
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
// This method will authenticate the newly created object using clientId, clientSecret and code.
// cacheFile is used to store code and only needs to be specified once. This method will return
// an error code indicating the URL to retrieve the 'code', if needed.
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

// splitPath will take a Unix like pathname, split it on its components, remove empty
// elements and return the directory, filename and a completely reconstructed path.
// The leading slash is removed, as well as any trailing slashes.
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
//	Gdrive Primitives: Direct interfaces with the Gdrive
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
// 'parentId'. The object's contents will come from 'localFile'.  If
// 'localFile' is not set, an empty object will be created (this is how we
// create directories). The title of the object will be set to 'title' and will
// default to the basename of the file if not set. The object's MIME Type will
// be set to 'mimeType', or automatically detected if mimeType is blank.
//
// This method returns a *drive.File object pointing to the file just inserted.
func (g *Gdrive) GdriveFilesInsert(localFile string, title string, parentId string, mimeType string) (*drive.File, error) {
	var err error
	var goFile *os.File
	var r *drive.File

	// Default title to basename of file
	if title == "" {
		title = path.Base(localFile)
	}
	driveFile := &drive.File{Title: title, MimeType: mimeType}
	if mimeType == "" {
		driveFile.MimeType = mime.TypeByExtension(path.Ext(localFile))
	}
	// Set parentId
	if parentId != "" {
		p := &drive.ParentReference{Id: parentId}
		driveFile.Parents = []*drive.ParentReference{p}
	}
	// Only insert file media if localFile is a filename
	if localFile != "" {
		goFile, err = os.Open(localFile)
		if err != nil {
			return nil, err
		}
		r, err = g.service.Files.Insert(driveFile).Media(goFile).Do()
	} else {
		r, err = g.service.Files.Insert(driveFile).Do()
	}
	if err != nil {
		return nil, err
	}
	return r, nil
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

// Insert a file named 'dstPath' with the contents of 'localFile'. This method
// will first insert the file under DRIVE_TMP_FOLDER and then move it to its
// final location. DRIVE_TMP_FOLDER will be automatically created, if needed.
// The inserted object's modifiedDate will be set to the mtime of localFile.
//
// The method returns a *drive.File object pointing to the file in its final
// location, or nil to indicate path related problems.
func (g *Gdrive) Insert(dstPath string, localFile string) (*drive.File, error) {
	// Sanitize
	_, dstFile, dstPath := splitPath(dstPath)
	if dstPath == "" {
		return nil, fmt.Errorf("Insert: empty destination path")
	}

	// We always upload to DRIVE_TMP_FOLDER so it must always exist
	tmpDirObj, err := g.Mkdir(DRIVE_TMP_FOLDER)
	if err != nil {
		return nil, err
	}
	if tmpDirObj == nil {
		return nil, fmt.Errorf("Insert: Unable to create temporary folder \"%s\"", DRIVE_TMP_FOLDER)
	}

	tmpFile := fmt.Sprintf("%s.%d", dstFile, g.gdrive_uid)
	tmpPath := DRIVE_TMP_FOLDER + "/" + tmpFile

	// Delete temp file if it already exists (file or directory)
	tmpFileObj, err := g.Stat(tmpPath)
	if err != nil {
		return nil, err
	}
	if tmpFileObj != nil {
		_, err = g.GdriveFilesTrash(tmpFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("Insert: Error removing existing temporary file \"%s\": %v", tmpPath, err)
		}
	}

	// Insert file into tmp dir with the temporary name
	tmpFileObj, err = g.GdriveFilesInsert(localFile, tmpFile, tmpDirObj.Id, "")
	if err != nil {
		return nil, fmt.Errorf("Insert: Error inserting temporary file \"%s\": %v", tmpPath)
	}

	// Move file to definitive location
	dstFileObj, err := g.Move(tmpPath, dstPath)
	if err != nil {
		return nil, err
	}
	if dstFileObj == nil {
		return nil, fmt.Errorf("Insert: Error moving tmp file \"%s\" to \"%s\"", tmpPath, dstPath)
	}

	// Set modified date to localFile's mtime
	fi, err := os.Stat(localFile)
	if err != nil {
		return nil, fmt.Errorf("Insert: Unable to stat localFile \"%s\": %v", localFile, err)
	}
	dstFileObj, err = g.SetModifiedDate(dstPath, fi.ModTime())
	if err != nil {
		return nil, fmt.Errorf("Insert: Unable to set date of \"%s\": %v", dstPath, err)
	}

	// No need to add to cache since Move (above) does it for us.
	return dstFileObj, nil
}

// Listdir returns a slice of *drive.File objects under 'drivePath'
// which match 'query' or nil if the path does not exist. If query is
// not specified, it defaults to 'trashed = false'.
func (g *Gdrive) Listdir(drivePath string, query string) ([]*drive.File, error) {
	var ret []*drive.File

	driveDir, err := g.Stat(drivePath)
	if err != nil || driveDir == nil {
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

// Mkdir creates the directory (folder) specified by drivePath. It returns nil if
// duplicate elements exist anywhere in the path or part of the path is missing or
// the *drive.File of the newly created directory. If a directory already exists
// with the same name, this method will return a *drive.File object pointing to it.
func (g *Gdrive) Mkdir(drivePath string) (*drive.File, error) {
	var parentId string

	// Sanitize
	pathname, dirname, drivePath := splitPath(drivePath)
	if drivePath == "" {
		return nil, fmt.Errorf("Mkdir: Attempting to create a blank directory")
	}

	// If the path already exists, returns a *drive.File
	// struct pointing to it.
	driveFile, err := g.Stat(drivePath)
	if err != nil || driveFile != nil {
		return driveFile, err
	}

	// If no path, start at root
	if pathname == "" {
		parentId = "root"
	} else {
		driveFile, err = g.Stat(pathname)
		if err != nil || driveFile == nil {
			return nil, err
		}
		parentId = driveFile.Id
	}

	driveFile, err = g.GdriveFilesInsert("", dirname, parentId, MIMETYPE_FOLDER)
	if err != nil {
		return nil, err
	}
	g.cacheAdd(drivePath, driveFile)
	return driveFile, nil
}

// Move the object in 'srcPath' (file or directory) to 'dstPath'.  It does that
// by removing the directory in srcPath from the list of parents of the object,
// and adding dstPath. The object will be renamed (meaning, it's possible to
// move and rename in a single operation.)
//
// Returns: *drive.File, err for the moved object.
func (g *Gdrive) Move(srcPath string, dstPath string) (*drive.File, error) {
	// Sanitize Source & Destination
	srcDir, _, srcPath := splitPath(srcPath)
	dstDir, dstFile, dstPath := splitPath(dstPath)

	if srcPath == "" || dstPath == "" {
		return nil, fmt.Errorf("Move: Source object and destination dir must be set")
	}

	// We need the source parentId, destination Id and object Id
	srcParentObj, err := g.Stat(srcDir)
	if err != nil {
		return nil, err
	}
	if srcParentObj == nil {
		return nil, fmt.Errorf("Move: Unable to find id for (parent dir) \"%s\"", srcDir)
	}

	srcObj, err := g.Stat(srcPath)
	if err != nil {
		return nil, err
	}
	if srcObj == nil {
		return nil, fmt.Errorf("Move: Unable to find object id for \"%s\"", srcPath)
	}

	dstDirObj, err := g.Stat(dstDir)
	if err != nil {
		return nil, err
	}
	if dstDirObj == nil {
		return nil, fmt.Errorf("Move: Unable to find object id for destination dir \"%s\"", dstDir)
	}

	dstFileObj, err := g.Stat(dstPath)
	if err != nil {
		return nil, err
	}
	if dstFileObj != nil {
		_, err = g.GdriveFilesTrash(dstFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("Move: Error removing destination file \"%s\": %v", dstPath, err)
		}
		g.cacheDel(dstPath)
	}

	// Set parents and change name if needed
	driveFile, err := g.GdriveFilesPatch(srcObj.Id, dstFile, "", []string{dstDirObj.Id}, []string{srcParentObj.Id})
	if err != nil {
		return nil, fmt.Errorf("Move: Error moving temporary file \"%s\" to \"%s\": %v", srcPath, dstPath, err)
	}
	g.cacheDel(srcPath)
	g.cacheAdd(dstPath, driveFile)
	return driveFile, nil
}

// Checks the remote dstPath's modification time and returns true if it is
// older than the mtime of localFile. It should be used to determine whether a
// file should be copied or not.
func (g *Gdrive) RemotePathOutdated(dstPath string, localFile string) (bool, error) {
	// Sanitize
	_, _, dstPath = splitPath(dstPath)
	if dstPath == "" {
		return false, fmt.Errorf("RemotePathOutdated: empty destination path")
	}

	fi, err := os.Stat(localFile)
	if err != nil {
		return false, fmt.Errorf("RemotePathOutdated: Unable to stat \"%s\": %v", localFile, err)
	}

	// We can only upload regular files (for now)
	if !fi.Mode().IsRegular() {
		return false, fmt.Errorf("RemotePathOutdated: We can only upload regular files (for now) \"%s\"", localFile)
	}

	dstPathObj, err := g.Stat(dstPath)
	if err != nil {
		return false, fmt.Errorf("RemotePathOutdated: Unable to stat remote path \"%s\": %v", dstPath, err)
	}
	// No remote copy means outdated
	if dstPathObj == nil {
		return true, nil
	}

	dstPathDate, err := ModifiedDate(dstPathObj)
	if err != nil {
		return false, fmt.Errorf("RemotePathOutdated: Unable to retrieve Modified date of \"%s\": %v", dstPath, err)
	}

	// Comparing with nanoseconds == rounding problems
	dstPathDate = dstPathDate.Truncate(time.Second)
	localFileDate := fi.ModTime().Truncate(time.Second)

	// We support copying to directories (they'll be erased by Insert)
	if localFileDate.After(dstPathDate) {
		return true, nil
	}

	return false, nil
}

// SetModifiedDate sets the modification date of the file/directory specified by
// 'drivePath' to 'modifiedDate'.
//
// Returns a *drive.File pointing to the modified file/dir and an error. If a
// non-fatal error occurs, *drive.File will be set to nil.
func (g *Gdrive) SetModifiedDate(drivePath string, modifiedDate time.Time) (*drive.File, error) {

	driveFile, err := g.Stat(drivePath)
	if err != nil {
		return nil, err
	}
	if driveFile == nil {
		return nil, fmt.Errorf("SetModifiedDate: Unable to fetch metadata for \"%s\"", drivePath)
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
// Since Google Drive allows more than one object with the same name and Unix
// filesystems do not, Stat will return a nil *drive.File object if duplicate
// elements are found anywhere along the path.
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
		if err != nil || len(children) == 0 {
			return nil, err
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
		if err != nil || len(children) == 0 {
			return nil, err
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

//------------------------------------------------------------------------------
// Helper functions
//
// There functions work on a *drive.File object, making commonly used
// accessed struct members available to the caller.
//-----------------------------------------------------------------------------

// IsDir returns true if the passed *drive.File object is a directory
func IsDir(driveFile *drive.File) bool {
	return (driveFile.MimeType == MIMETYPE_FOLDER)
}

// CreateDate returns the time.Time representation of the *drive.File object's creation date.
func CreateDate(driveFile *drive.File) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, driveFile.CreatedDate)
}

// ModifiedDate returns the time.Time representation of the *drive.File object's modification date.
func ModifiedDate(driveFile *drive.File) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, driveFile.ModifiedDate)
}
