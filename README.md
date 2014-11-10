# Gdrive_path

Gdrive_path (GDP) is a Go library that allows easy access to files and
directories (aka "folders") on Google Drive. The library abstracts Google
Drive concepts by presenting a "path" like interface, allowing users to access their
files in Google Drive using Unix-like pathnames.

## Installation

To compile this program and use the gdrive_path libraries, you need a few things:

Create a working directory for this project:

    $ mkdir ~/go
    $ mkdir ~/go/src

Make sure your GOPATH environment variable points to the correct location:

    $ export GOPATH=~/go

Install the necessary packages:

    $ go get code.google.com/p/google-api-go-client/drive/v2
    $ go get code.google.com/p/goauth2/oauth

Compile with go build as usual.

## Google Drive instructions

To use Google Drive with this library, you need a Client Id, a Client Secret and a
one-time code. To create the Client Id and Secret, visit the [Google Developers Console](https://console.developers.google.com/project) and create a new project.
Make sure the project has the Google Drive API turned on (APIs & Auth/APIs menu on the left).
Use the APIs & Auth/Credentials menu to create a new Client ID for OAuth authentication.

For more information on the entire process, consult the [Google Drive Web APIs](https://developers.google.com/drive/web/enable-sdk) page.

## Example

To run the program below, you need a "Client Id" and "Secret" (see instructions on how to
obtain those in the "Google Drive instructions" section above). Run the program with the
--id and --secret options, passing those values. The program will show an URL where
the "code" can be obtained and exit. Use a browser to open that link and re-run the program with the
--code option. From this point on, it will not be necessary to specify --id, --secret or --code.

```go
package main

import (
        "flag"
        "fmt"
        "log"
        "os"
        "os/user"
        "path"
        "time"

        drive "code.google.com/p/google-api-go-client/drive/v2"
        gdp "github.com/marcopaganini/gdrive_path"
)

const (
        // Oauth cache file. Relative to the user's homedir
        authCacheFile = ".gdrive_example_auth.cache"

        // Our base directory inside Gdrive
        gdriveBaseDir = "testdir"
)

var (
        clientId     = flag.String("id", "", "Client ID")
        clientSecret = flag.String("secret", "", "Client Secret")
        requestURL   = flag.String("request_url", "https://www.googleapis.com/oauth2/v1/userinfo", "API request")
        code         = flag.String("code", "", "Authorization Code")
)
func main() {
        flag.Parse()

        usr, err := user.Current()
        if err != nil {
                log.Fatalf("Unable to get user information")
        }
        cachefile := path.Join(usr.HomeDir, authCacheFile)

        g, err := gdp.NewGdrivePath(*clientId, *clientSecret, *code, drive.DriveScope, cachefile)
        if err != nil {
                log.Fatalf("Unable to initialize GdrivePath: %v", err)
        }

        // Create a few directories for no good reason other than
        // show that we refer to files using familiar pathnames.
        dirs := [...]string{gdriveBaseDir, path.Join(gdriveBaseDir, "test1"), path.Join(gdriveBaseDir, "test2")}
        for _, d := range dirs {
                _, err := g.Mkdir(d)
                if err != nil {
                        log.Fatalf("Unable to create directory \"%s\", error %v\n", d, err)
                }
        }

        // Insert the /etc/group file into the newly created directories
        remoteFile := path.Join(gdriveBaseDir, "group")
        localFile := "/etc/group"

        r, err := os.Open(localFile)
        if err != nil {
                log.Fatalf("Unable to open", localFile)
        }
        defer r.Close()

        // Insert the file into Google Gdrive
        _, err = g.InsertInPlace(remoteFile, r)
        if err != nil {
                log.Fatalf("Error inserting \"%s\": %v", remoteFile, err)
        }

        // List the contents of the newly created directory
        dirlist, err := g.ListDir(gdriveBaseDir, "")
        if err != nil {
                log.Fatalf("Error listing directory \"%s\": %v", gdriveBaseDir, err)
        }

        for _, fileObj := range dirlist {
                filetype := "[file] "
                if gdp.IsDir(fileObj) {
                        filetype = "[dir]  "
                }
                create, _ := gdp.CreateDate(fileObj)
                modify, _ := gdp.CreateDate(fileObj)

                fmt.Printf("%s %s %s [%s]\n",
                        filetype,
                        create.Format(time.UnixDate),
                        modify.Format(time.UnixDate),
                        fileObj.Title)
        }
}
```

## Notes

This library should be considered in BETA state. I'm actively working on it. Please keep the following in mind
when using this it:

* Google Drive allows multiple files/directories with the same name. Since
  we're (kinda) emulating the semantics of a Unix filesystem, the library will
  return an error if it finds duplicates. It's up to the user to clean the
  files manually. Every effort has been made to prevent this condition, but
  there are certainly bugs lurking around.

* Since Google Drive was not designed to be used with "paths", the library
  needs to make many Google Drive native calls, even for simple operations
  (anything using a path needs information about every element on the path.
  I've added caching to the library to make things better.

## Author

(C) 2014 by Marco Paganini <paganini AT paganini DOT net>
