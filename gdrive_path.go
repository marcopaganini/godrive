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
	"log"
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
}

// NewGdrivePath creates and returns a new *Gdrive Object.
func NewGdrivePath(clientId string, clientSecret string, code string, scope string, cacheFile string) (*Gdrive, error) {
	if clientId == "" || clientSecret == "" {
		return nil, fmt.Errorf("Need both clientId and clientSecret")
	}

	g := &Gdrive{clientId: clientId, clientSecret: clientSecret, code: code, scope: scope, cacheFile: cacheFile}
	err := g.authenticate()
	if err != nil {
		return nil, err
	}
	g.client = g.transport.Client()
	g.service, err = drive.New(g.client)
	return g, err
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
			return fmt.Errorf("Code missing. To get a new one visit the url below:\n%s", url)
		}
		// Exchange the authorization code for an access token.
		// ("Here's the code you gave the user, now give me a token!")
		// If everything works, the Exchange method will cache the token.
		token, err = g.transport.Exchange(g.code)
		if err != nil {
			return fmt.Errorf("Error exchanging code for token: %v", err)
		}
	}

	g.transport.Token = token
	return nil
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

	// Sanitize
	dirs, filename, drivePath := splitPath(drivePath)
	if drivePath == "" {
		return nil, fmt.Errorf("Trying to stat blank path")
	}

	parent := "root"

	// We make sure that:
	// - Every element in our path exists
	// - Every element in our path is a directory
	// - No duplicates exist anywhere in the path
	//
	// Note: this is expensive for what it is :(

	// fmt.Printf("====\nDEBUG Stat dirs=[%s], filename=[%s]\n", dirs, filename)
	for _, elem := range strings.Split(dirs, "/") {
		// Split on blank strings returns one element
		if elem == "" {
			continue
		}

		// fmt.Printf("DEBUG: Testing elem [%s]\n", elem)
		// Test: No elements in our directory path are files
		query = fmt.Sprintf("title = '%s' and trashed = false and mimeType != '%s'", elem, MIMETYPE_FOLDER)
		children, err = g.GdriveChildrenList(parent, query)
		// fmt.Printf("DEBUG Stat test file for parent [%s] returned err [%v] children [%v]\n", parent, err, children)
		if err != nil {
			return nil, err
		}
		if len(children) != 0 {
			return nil, fmt.Errorf("Element \"%s\" in path \"%s\" is a file, not a directory", elem, drivePath)
		}

		// Test: One and only one directory
		query = fmt.Sprintf("title = '%s' and trashed = false and mimeType = '%s'", elem, MIMETYPE_FOLDER)
		children, err = g.GdriveChildrenList(parent, query)
		// fmt.Printf("DEBUG Stat test dir for parent [%s] returned err [%v] children [%v]\n", parent, err, children)
		if err != nil || len(children) == 0 {
			return nil, err
		}
		if len(children) > 1 {
			return nil, fmt.Errorf("More than one directory \"%s\" exists in path \"%s\"", elem, drivePath)
		}
		parent = children[0].Id
		// fmt.Printf("DEBUG: Using parent [%s]\n", parent)
	}

	// At this point, the entire path is good. We now check for 'filename'
	// (which is really the last element in our path). It coud be a file or
	// a directory, but duplicates are not supported.

	if filename != "" {
		query = fmt.Sprintf("title = '%s' and trashed = false", filename)
		children, err = g.GdriveChildrenList(parent, query)
		// fmt.Printf("DEBUG Stat test filename for parent [%s] returned err [%v] children [%v]\n", parent, err, children)
		if err != nil || len(children) == 0 {
			return nil, err
		}
		if len(children) > 1 {
			return nil, fmt.Errorf("More than one file/directory \"%s\" exists in path \"%s\"", filename, drivePath)
		}
		parent = children[0].Id
		// fmt.Printf("DEBUG: Using parent [%s]\n", parent)
	}

	// Parent contains the id of the last element

	ret, err := g.GdriveFilesGet(parent)
	// fmt.Printf("DEBUG returning ret=[%v] err=[%v]\n", ret, err)
	return ret, err
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
		return nil, fmt.Errorf("Attempting to create a blank directory")
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
	return driveFile, nil
}

// Move will move the object in 'srcPath' (file or directory) to have 'dstDir'
// as its new parent. The original parentId will be removed (effectively moving the file
// to another directory). This method returns a *drive.File object pointing to the
// moved file or nil in case of path problems (duplicate elements, non-existing path, etc)
func (g *Gdrive) Move(srcPath string, dstDir string) (*drive.File, error) {
	// Sanitize Source & Destination
	srcDir, srcFile, srcPath := splitPath(srcPath)
	_, _, dstDir = splitPath(dstDir)

	if srcPath == "" || dstDir == "" {
		return nil, fmt.Errorf("Move: Source Object and Destination Dir must be set")
	}

	// We need the source parentId, destination Id and object Id
	// fmt.Printf("DEBUG source parent is %s\n", srcDir)
	srcParentObj, err := g.Stat(srcDir)
	if err != nil || srcParentObj == nil {
		return nil, fmt.Errorf("Unable to find id for (parent dir) \"%s\": %v", srcDir, err)
	}
	// fmt.Printf("DEBUG source is %s\n", srcPath)
	srcObj, err := g.Stat(srcPath)
	if err != nil || srcObj == nil {
		return nil, fmt.Errorf("Unable to find object id for \"%s\": %v", srcPath, err)
	}
	// fmt.Printf("DEBUG dest is %s\n", dstDir)
	dstDirObj, err := g.Stat(dstDir)
	if err != nil || dstDirObj == nil {
		return nil, fmt.Errorf("Unable to find object id for destination dir \"%s\": %v", dstDir, err)
	}

	// If we have the same filename inside the destination directory:
	// - If it is a directory, abort the move (we can only overwrite files)
	// - If it is a file, trash the destination first
	dstPath := dstDir + "/" + srcFile
	dstFileObj, err := g.Stat(dstPath)
	if err != nil {
		return nil, fmt.Errorf("Error fetching metadata for \"%s\": %v", dstPath, err)
	}
	// log.Printf("DEBUG: Got metadata for file [%s]", dstPath)
	if dstFileObj != nil {
		if IsDir(dstFileObj) {
			return nil, fmt.Errorf("Cannot move \"%s\" to \"%s\" (latter is a directory)", srcPath, dstDir)
		}
		_, err = g.GdriveFilesTrash(dstFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("Error removing \"%s\": %v", dstPath, err)
		}
	}
	// log.Printf("DEBUG: Finished trash testing")

	// Set parents
	driveFile, err := g.GdriveFilesPatch(srcObj.Id, []string{dstDirObj.Id}, []string{srcParentObj.Id})
	if err != nil {
		log.Fatalf("Error moving file \"%s\" to \"%s\": %v", srcPath, dstDir, err)
	}
	// log.Printf("DEBUG: Finished Patching")
	// log.Printf("DEBUG: Got drivefile after move: %v", driveFile)
	return driveFile, nil
}

// Insert a file named 'dstPath' with the contents of 'localFile'. This method will first
// insert the file under DRIVE_TMP_FOLDER and then move it to its final location. DRIVE_TMP_FOLDER
// will be automatically created, if needed. The method returns a *drive.File object pointing to
// the file in its final location, or nil to indicate path related problems.
func (g *Gdrive) Insert(dstPath string, localFile string) (*drive.File, error) {
	// Sanitize
	dstDir, dstFile, dstPath := splitPath(dstPath)
	if dstPath == "" {
		return nil, fmt.Errorf("Attempting to upload to a blank path")
	}

	// We always upload to DRIVE_TMP_FOLDER so it must always exist
	tmpDirObj, err := g.Mkdir(DRIVE_TMP_FOLDER)
	if err != nil || tmpDirObj == nil {
		return nil, fmt.Errorf("Unable to create temporary folder \"%s\"", DRIVE_TMP_FOLDER)
	}

	// Delete temp file if it already exists (file or directory)
	tmpPath := DRIVE_TMP_FOLDER + "/" + dstFile

	tmpFileObj, err := g.Stat(tmpPath)
	if err != nil {
		log.Fatalf("Error getting metadata for \"%s\": %v", tmpPath, err)
	}
	if tmpFileObj != nil {
		_, err = g.GdriveFilesTrash(tmpFileObj.Id)
		if err != nil {
			return nil, fmt.Errorf("Error removing temporary file \"%s\": %v", tmpPath, err)
		}
	}

	// Insert file into tmp dir
	tmpFileObj, err = g.GdriveFilesInsert(localFile, "", tmpDirObj.Id, "")
	if err != nil {
		return nil, fmt.Errorf("Error inserting temporary file \"%s\": %v", tmpPath)
	}

	// Move file to definitive location
	// log.Printf("DEBUG: now moving %s to %s\n", tmpPath, dstDir)
	dstFileObj, err := g.Move(tmpPath, dstDir)
	if err != nil || dstFileObj == nil {
		return nil, fmt.Errorf("Error moving tmp file \"%s\" to \"%s\": %v", tmpPath, dstPath, err)
	}

	return dstFileObj, nil
}

//
// Helper functions
//
// There functions work on a *drive.File object, making commonly used
// accessed struct members available to the caller.
//

// IsDir returns true if the passed *drive.File object is a directory
func IsDir(driveFile *drive.File) bool {
	return (driveFile.MimeType == MIMETYPE_FOLDER)
}

// CreateDate returns the time.Time representation of the *drive.File object's creation date.
func CreateDate(driveFile *drive.File) (time.Time, error) {
	return time.Parse(time.RFC3339, driveFile.CreatedDate)
}

// ModifiedDate returns the time.Time representation of the *drive.File object's modification date.
func ModifedDate(driveFile *drive.File) (time.Time, error) {
	return time.Parse(time.RFC3339, driveFile.ModifiedDate)
}

// GdriveFilesGet Returns a *drive.File object for the object identified by 'fileId'
func (g *Gdrive) GdriveFilesGet(fileId string) (*drive.File, error) {
	f, err := g.service.Files.Get(fileId).Do()
	if err != nil {
		return nil, fmt.Errorf("Error retrieving File Metadata for fileId \"%s\": %v", fileId, err)
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
			return nil, fmt.Errorf("Error \"%v\" fetching Id for parent_id \"%s\", query=\"%s\"", err, parentId, query)
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
	// Default title to basename of file
	if title == "" {
		title = path.Base(localFile)
	}
	driveFile := &drive.File{Title: title}
	if mimeType != "" {
		driveFile.MimeType = mimeType
	}
	// Set parentId
	if parentId != "" {
		p := &drive.ParentReference{Id: parentId}
		driveFile.Parents = []*drive.ParentReference{p}
	}
	// Only insert file media if localFile is a filename
	if localFile != "" {
		goFile, err := os.Open(localFile)
		if err != nil {
			return nil, err
		}
		g.service.Files.Insert(driveFile).Media(goFile)
	}
	r, err := g.service.Files.Insert(driveFile).Do()
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GdriveFilesPatch adds and/or remove parents to the file specified by 'fileId'. It returns
// a *drive.File object pointing to the modified file.
func (g *Gdrive) GdriveFilesPatch(fileId string, addParentIds []string, removeParentIds []string) (*drive.File, error) {
	driveFile := &drive.File{}
	p := g.service.Files.Patch(fileId, driveFile)
	if len(addParentIds) > 0 {
		p.AddParents(strings.Join(addParentIds, ","))
	}
	if len(removeParentIds) > 0 {
		p.RemoveParents(strings.Join(removeParentIds, ","))
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
