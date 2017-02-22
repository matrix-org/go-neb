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
	searchText := "Czechoslovakian bananna"
	wikipediaAPIURL := "https://en.wikipedia.org/w/api.php"

	// Mock the response from Wikipedia
	wikipediaTrans := testutils.NewRoundTripper(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()

		// Check the base API URL
		if !strings.HasPrefix(req.URL.String(), wikipediaAPIURL) {
			t.Fatalf("Bad URL: got %s want prefix %s", req.URL.String(), wikipediaAPIURL)
		}
		// Check the request method
		if req.Method != "GET" {
			t.Fatalf("Bad method: got %s want GET", req.Method)
		}
		// Check the search query
		// Example query - https://en.wikipedia.org/w/api.php?action=query&prop=extracts&format=json&exintro=&titles=RMS+Titanic
		var searchString = query.Get("titles")
		var searchStringLength = len(searchString)
		if searchStringLength > 0 && searchString != searchText {
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
	srv, err := types.CreateService("id", ServiceType, "@wikipediabot:hyrule", []byte(`{}`))
	if err != nil {
		t.Fatal("Failed to create Wikipedia service: ", err)
	}
	wikipedia := srv.(*Service)

	// Mock the response from Matrix
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Unknown URL: %s", req.URL.String())
	}
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@wikipediabot:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// Execute the matrix !command
	cmds := wikipedia.Commands(matrixCli)
	if len(cmds) != 1 {
		t.Fatalf("Unexpected number of commands: %d", len(cmds))
	}
	cmd := cmds[0]
	_, err = cmd.Command("!someroom:hyrule", "@navi:hyrule", []string{searchText})
	if err != nil {
		t.Fatalf("Failed to process command: %s", err.Error())
	}
}
