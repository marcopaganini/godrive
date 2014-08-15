# Gdrive_path

Gdrive_path (GDP) is a Go library that allows easy access to files and
directories (aka "folders") in a Google Drive. It attempts to abstract Google
Drive concepts by presenting a "path" like interface. In other words, it allows
users to access their files in Google Drive using regular paths, similar to a
regular Unix filesystem.

There's usually no need to worry about parent_ids and object_ids. Finding
information on an object is as simple as doing a Stat("/path/to/file" and
interpreting the resulting drive.File (google drive native) object.

Besides the "high level" function, most "glue" functions to the lower level
Gdrive primitives are also exported, so they can be used as well.

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

Compile with go build

## Google Drive instructions

To use Google Drive, you first need to have a Client Id, a Client Secret and a
one-time code. To create the Client Id and Secret, visit:

    https://developers.google.com/drive/web/enable-sdk

## Example Program

You can get an example program showing some of the features with:

    $ go get github.com/marcopaganini/gdrive_path_example

Follow the instructions in the README.md from that repository to complile.

## Notes

This code is VERY ALPHA but I'm actively working on it. Keep the following in mind
when using this library:

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
