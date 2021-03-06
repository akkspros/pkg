package zip

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Zip describes a zip file.
type Zip struct {
	url      string
	dest     string
	files    []string
	checksum string
}

var (
	// File system operation variables.
	createFile       = os.Create
	makeDirectoryAll = os.MkdirAll
	ioCopy           = io.Copy
	openFile         = os.OpenFile

	// Default paths.
	sourceFilename = "source.zip"
)

// PrepareFiles downloads a zip file to a given destination and extracts info about the files in the zip.
func (m *Zip) PrepareFiles(dest string) error {

	// Prepare destination.
	m.dest = dest
	if _, err := os.Stat(m.dest); os.IsNotExist(err) {
		os.Mkdir(m.dest, os.ModePerm)
	}

	err := downloadFile(m.url, m.dest+"/"+sourceFilename)
	if err != nil {
		return err
	}

	var checksums []string
	m.files, checksums, err = unzip(m.dest+"/"+sourceFilename, m.dest+"/unzipped")
	if err != nil {
		return err
	}

	// Calculate checksum - uses same technique as Tide Audit Server.
	m.checksum = combinedChecksum(checksums)

	return nil
}

// GetChecksum returns the combined checksum for the zip file.
func (m Zip) GetChecksum() string {
	return m.checksum
}

// GetFiles returns the files contained in the zip file.
func (m Zip) GetFiles() []string {
	return m.files
}

// NewZip returns a new Zip source.
func NewZip(url string) *Zip {
	return &Zip{
		url: url,
	}
}

// downloadFile uses an HTTP request to get a file and save it to a given destination folder.
func downloadFile(source string, destination string) error {

	// Create destination
	out, err := createFile(destination)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get file
	resp, err := http.Get(source)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Write to file
	_, err = ioCopy(out, resp.Body)

	if err != nil {
		return err
	}

	return nil
}

// unzip will un-compress a zip archive,
// moving all files and folders to a destination directory
//
// Props to https://golangcode.com/unzip-files-in-go/ and
// http://blog.ralch.com/tutorial/golang-working-with-zip/
func unzip(source, destination string) (filenames, checksums []string, err error) {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return filenames, checksums, err
	}

	if err := makeDirectoryAll(destination, 0755); err != nil {
		return filenames, checksums, err
	}

	rootPath := ""
	for _, file := range reader.File {
		path := file.Name
		if !file.FileInfo().IsDir() {
			continue
		}
		if len(path) < len(rootPath) || rootPath == "" {
			rootPath = path
		}
	}

	for _, file := range reader.File {
		path := filepath.Join(destination, strings.TrimPrefix(file.Name, rootPath))
		if file.FileInfo().IsDir() {
			makeDirectoryAll(path, file.Mode())
			continue
		}

		filenames = append(filenames, path)

		// This reads the file from the ZIP. It does not yet exist on the system.
		fileReader, _ := file.Open()

		h := sha256.New()
		if _, err := ioCopy(h, fileReader); err != nil {
			fileReader.Close()
			return nil, nil, err
		}
		checksums = append(checksums, fmt.Sprintf("%x", h.Sum(nil)))

		targetFile, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			fileReader.Close()
			return nil, nil, err
		}

		// Because the zip package does not implement Seek(), we need to read it again..
		fileReader, _ = file.Open()

		if _, err := ioCopy(targetFile, fileReader); err != nil {
			fileReader.Close()
			targetFile.Close()
			return nil, nil, err
		}

		fileReader.Close()
		targetFile.Close()
	}

	return filenames, checksums, err
}

func combinedChecksum(sums []string) string {
	sort.Strings(sums)
	jsonChecksums, _ := json.Marshal(sums)
	return fmt.Sprintf("%x", sha256.Sum256(jsonChecksums))
}
