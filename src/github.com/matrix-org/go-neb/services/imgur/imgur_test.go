package imgur

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/testutils"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

func TestCommand(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})
	clientID := "My ID"
	imgurImageURL := "http://i.imgur.com/cat.jpg"
	testSearchString := "Czechoslovakian bananna"

	// Mock the response from imgur
	imgurTrans := testutils.NewRoundTripper(func(req *http.Request) (*http.Response, error) {
		imgurURL := "https://api.imgur.com/3/gallery/search"
		query := req.URL.Query()

		// Check the base API URL
		if !strings.HasPrefix(req.URL.String(), imgurURL) {
			t.Fatalf("Bad URL: got %s want prefix %s", req.URL.String(), imgurURL)
		}
		// Check the request method
		if req.Method != "GET" {
			t.Fatalf("Bad method: got %s want GET", req.Method)
		}
		// Check the Client ID
		authHeader := req.Header.Get("Authorization")
		if authHeader != "Client-ID "+clientID {
			t.Fatalf("Bad client ID - Expected: %s, got %s", "Client-ID "+clientID, authHeader)
		}

		// Check the search query
		var searchString = query.Get("q")
		if searchString != testSearchString {
			t.Fatalf("Bad search string - got: \"%s\", expected: \"%s\"", testSearchString, searchString)
		}

		img := imgurGalleryImage{
			Title:   "A Cat",
			Link:    imgurImageURL,
			Type:    "image/jpeg",
			IsAlbum: func() *bool { b := false; return &b }(),
		}

		imgJSON, err := json.Marshal(img)
		if err != nil {
			t.Fatalf("Failed to Marshal test image data - %s", err)
		}
		rawImageJSON := json.RawMessage(imgJSON)

		res := imgurSearchResponse{
			Data: []json.RawMessage{
				rawImageJSON,
			},
			Success: func() *bool { b := true; return &b }(),
			Status:  200,
		}

		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("Failed to marshal imgur response - %s", err)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
		}, nil
	})
	// clobber the imgur service http client instance
	httpClient = &http.Client{Transport: imgurTrans}

	// Create the imgur service
	srv, err := types.CreateService("id", ServiceType, "@imgurbot:hyrule", []byte(
		fmt.Sprintf(`{
			"client_id":"%s"
		}`, clientID),
	))
	if err != nil {
		t.Fatal("Failed to create imgur service: ", err)
	}
	imgur := srv.(*Service)

	// Mock the response from Matrix
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == imgurImageURL { // getting the imgur image
			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString("some image data")),
			}, nil
		} else if strings.Contains(req.URL.String(), "_matrix/media/r0/upload") { // uploading the image to matrix
			return &http.Response{
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString(`{"content_uri":"mxc://foo/bar"}`)),
			}, nil
		}
		return nil, fmt.Errorf("Unknown URL: %s", req.URL.String())
	}
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@imgurbot:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// Execute the matrix !command
	cmds := imgur.Commands(matrixCli)
	if len(cmds) != 2 {
		t.Fatalf("Unexpected number of commands: %d", len(cmds))
	}
	cmd := cmds[1]
	_, err = cmd.Command("!someroom:hyrule", "@navi:hyrule", []string{testSearchString})
	if err != nil {
		t.Fatalf("Failed to process command: %s", err.Error())
	}
}
