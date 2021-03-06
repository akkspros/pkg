package process

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/wptide/pkg/log"
	"github.com/wptide/pkg/message"
	"github.com/wptide/pkg/source"
	"github.com/wptide/pkg/source/zip"
)

// Ingest defines the structure for our Ingest process.
type Ingest struct {
	Process                              // Inherits methods from Process.
	In            <-chan message.Message // Expects a message channel as input.
	Out           chan Processor         // Send results to an output channel.
	TempFolder    string                 // Path to a temp folder where files will be extracted.
	sourceManager source.Source          // Responsible for getting the code to audit.
}

// Run executes the process in the pipeline.
func (ig *Ingest) Run(errc *chan error) error {
	// If we don't have a temp folder, then we need a fatal.
	if ig.TempFolder == "" {
		return errors.New("no temp folder provided for processes")
	}
	if ig.In == nil {
		return errors.New("no message channel to ingest")
	}
	if ig.Out == nil {
		return errors.New("requires a next process")
	}

	go func() {
		for {
			select {
			case msg := <-ig.In:

				// Init the Result object.
				ig.Result = &Result{}

				// If message is invalid, skip it, but keep listening on the channel.
				if err := validateMessage(msg); err != nil {
					// Pass the error up the error channel.
					*errc <- errors.New("Ingest Error: " + err.Error())

					// continue so that the message doesn't get passed along.
					continue
				}

				// Get the original message.
				ig.SetMessage(msg)

				// Run the process.
				// If processing produces an error send it up the error channel.
				if err := ig.Do(); err != nil {
					// Pass the error up the error channel.
					*errc <- errors.New("Ingest Error: " + err.Error())

					// continue so that the message doesn't get passed along.
					continue
				}

				// Send process to the out channel.
				ig.Out <- ig
			}
		}

	}()

	return nil
}

// Do runs the actual code for this process.
func (ig *Ingest) Do() error {

	log.Log(ig.Message.Title, "Ingesting...")

	// Set the source manager based on message.
	switch source.GetKind(ig.Message.SourceURL) {
	case "zip":
		ig.sourceManager = zip.NewZip(ig.Message.SourceURL)
	}

	// Return an error if we don't have a source manager.
	if ig.sourceManager == nil {
		return ig.Error("could not get appropriate source manager to handle ingest")
	}

	// Calculate hash of the source url.
	hasher := sha256.New()
	hasher.Write([]byte(ig.Message.SourceURL))

	// Set the path to where we will extract the files.
	ig.SetFilesPath(ig.TempFolder + "/audit-" + base64.URLEncoding.EncodeToString(hasher.Sum(nil)))

	// Download/Prepare the files.
	err := ig.sourceManager.PrepareFiles(ig.GetFilesPath())
	if err != nil {
		return err
	}

	// Project checksum.
	checksum := ig.sourceManager.GetChecksum()
	if checksum == "" {
		return ig.Error("could not calculate project checksum")
	}

	// Populate the result.
	result := *ig.Result
	result["checksum"] = checksum
	result["files"] = ig.sourceManager.GetFiles()
	result["filesPath"] = ig.GetFilesPath()
	ig.Result = &result

	log.Log(ig.Message.Title, "Project checksum: `"+checksum+"`")

	return nil
}

// validateMessage ensures that a message to be processed has the minimum requirements.
func validateMessage(msg message.Message) error {

	// A message should provide a title.
	if msg.Title == "" {
		return errors.New("message does not have a title")
	}

	// A message requires an endpoint to send results back to.
	if msg.ResponseAPIEndpoint == "" {
		return errors.New(msg.Title + ": does not provide an endpoint")
	}

	// A message must have a source url to process.
	if msg.SourceURL == "" {
		return errors.New(msg.Title + ": source url is empty")
	}

	// A message must provide the type of source to process.
	if msg.SourceType == "" {
		return errors.New(msg.Title + ": source type is empty (e.g. zip, git)")
	}

	return nil
}
