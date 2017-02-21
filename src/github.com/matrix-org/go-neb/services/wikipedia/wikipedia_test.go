package wikipedia

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

// TODO: It would be nice to tabularise this test so we can try failing different combinations of responses to make
//       sure all cases are handled, rather than just the general case as is here.
func TestCommand(t *testing.T) {
	database.SetServiceDB(&database.NopStorage{})
	apiKey := "secret"
	wikipediaImageURL := "https://www.wikipediaapis.com/customsearch/v1"

	// Mock the response from Wikipedia
	wikipediaTrans := testutils.NewRoundTripper(func(req *http.Request) (*http.Response, error) {
		wikipediaURL := "https://www.wikipediaapis.com/customsearch/v1"
		query := req.URL.Query()

		// Check the base API URL
		if !strings.HasPrefix(req.URL.String(), wikipediaURL) {
			t.Fatalf("Bad URL: got %s want prefix %s", req.URL.String(), wikipediaURL)
		}
		// Check the request method
		if req.Method != "GET" {
			t.Fatalf("Bad method: got %s want GET", req.Method)
		}
		// Check the API key
		if query.Get("key") != apiKey {
			t.Fatalf("Bad apiKey: got %s want %s", query.Get("key"), apiKey)
		}
		// Check the search query
		var searchString = query.Get("q")
		var searchStringLength = len(searchString)
		if searchStringLength > 0 && !strings.HasPrefix(searchString, "image") {
			t.Fatalf("Bad search string: got \"%s\" (%d characters) ", searchString, searchStringLength)
		}

		page := wikipediaPage{
			PageID:    1,
			NS:        1,
			Title:     "Test page",
			Touched:   "2017-02-21 00:00:00",
			LastRevID: 1,
			Extract:   "Some extract text",
		}
		pages := map[string]wikipediaPage{
			"1": page,
		}
		res := wikipediaSearchResults{
			Query: wikipediaQuery{
				Pages: pages,
			},
		}

		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("Failed to marshal Wikipedia response - %s", err)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
		}, nil
	})
	// clobber the Wikipedia service http client instance
	httpClient = &http.Client{Transport: wikipediaTrans}

	// Create the Wikipedia service
	srv, err := types.CreateService("id", ServiceType, "@wikipediabot:hyrule", []byte(fmt.Sprintf(`{"api_key":"%s"}`, apiKey)))
	if err != nil {
		t.Fatal("Failed to create Wikipedia service: ", err)
	}
	wikipedia := srv.(*Service)

	// Mock the response from Matrix
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == wikipediaImageURL { // getting the Wikipedia image
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
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@wikipediabot:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// Execute the matrix !command
	cmds := wikipedia.Commands(matrixCli)
	if len(cmds) != 1 {
		t.Fatalf("Unexpected number of commands: %d", len(cmds))
	}
	// cmd := cmds[0]
	// _, err = cmd.Command("!someroom:hyrule", "@navi:hyrule", []string{"image", "Czechoslovakian bananna"})
	// if err != nil {
	// 	t.Fatalf("Failed to process command: %s", err.Error())
	// }
}
