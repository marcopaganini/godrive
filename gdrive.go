package gdrive_path

// This library requires the Google Drive SDK to run.
//
// For details, check the README.md file with this distribution.
//
// This file contains the core of the methods interfacing with Gdrive itself
// and lower level methods. Most users will only use the "NewGdrivePath" method
// from this file and use the higher level routines in path.go
//
// This library is under constant and rapid development but should be
// considered ALPHA quality for the time being. The author will not be help
// responsible if it eats all of your files, kicks your cat and runs away with
// you wife/husband.
//
// (C) Aug/2014 by Marco Paganini <paganini@paganini.net>

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/marcopaganini/logger"

	oauth "code.google.com/p/goauth2/oauth"
	drive "code.google.com/p/google-api-go-client/drive/v2"
)

const (
	// Mime-Type used by Google Drive to indicate a folder
	MIMETYPE_FOLDER = "application/vnd.google-apps.folder"

	// Directory in Google Drive to hold temporary copies of files during inserts
	DRIVE_TMP_FOLDER = "tmp"

	// Total number of tries when we get a 5xx from Gdrive (includes first attempt)
	NUM_TRIES = 3
)

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

	log *logger.Logger

	// caches (one for Drive.File objects, another for child objects)
	filecache  *map[string]*objCache
	childcache *map[string]*objCache
}

// NewGdrivePath creates and returns a new *Gdrive Object or (nil, error) in case of problems.
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

	// Logger method
	g.log = logger.New("")

	// Initialize blank caches
	g.filecache = &map[string]*objCache{}
	g.childcache = &map[string]*objCache{}

	return g, err
}

// Authenticates a newly created method (called by NewGdrivePath).
//
// This method authenticates the newly created object using clientId,
// clientSecret and code.  cacheFile is used to store code and only needs to be
// specified once.
//
// Returns an error if the authentication process requires the user to fetch a
// new code. The error message contains the URL to be used to fetch a new auth
// code.
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
//	Gdrive Primitives: Direct interfaces with Gdrive
//------------------------------------------------------------------------------

// Return a *drive.File object for the object identified by 'fileId'
func (g *Gdrive) GdriveFilesGet(fileId string) (*drive.File, error) {
	f, err := driveFileOpRetry(g.service.Files.Get(fileId).Do)
	if err != nil {
		return nil, fmt.Errorf("GdriveFilesGet: Error retrieving File Metadata for fileId \"%s\": %v", fileId, err)
	}
	return f, nil
}

// Return a slice of *drive.ChilReference containing all
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
		r, err := driveChildListOpRetry(c.Do)
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

// Insert a new Object (file/dir) on Google Drive under 'parentId'. The
// object's contents will come from 'reader' (io.Reader). If reader is nil, an
// empty object will be created (this is how we create directories). The title
// of the object will be set to 'title' and the object's MIME Type will be set
// to 'mimeType', or automatically detected if mimeType is blank.
//
// Returns a *drive.File object pointing to the file just inserted.
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
		ret, err = driveFileOpRetry(g.service.Files.Insert(driveFile).Media(reader).Do)
	} else {
		ret, err = driveFileOpRetry(g.service.Files.Insert(driveFile).Do)
	}
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// Patches a Gdrive object metadata. Currently it can change the Title,
// modifiedDate, and the list of parent Ids.  Setting values to a blank string
// (when of type string) or an empty slice (type slice) will cause that
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
	r, err := driveFileOpRetry(p.Do)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Moves the object indicated by 'fileId' to the Google Drive Trash.
// Returns a *drive.File object pointing to the file inside Trash.
func (g *Gdrive) GdriveFilesTrash(fileId string) (*drive.File, error) {
	return driveFileOpRetry(g.service.Files.Trash(fileId).Do)
}
