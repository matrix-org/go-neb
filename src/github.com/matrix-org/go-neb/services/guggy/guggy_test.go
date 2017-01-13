package guggy

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
	guggyImageURL := "https://guggy.com/gifs/23ryf872fg"

	// Mock the response from Guggy
	guggyTrans := testutils.NewRoundTripper(func(req *http.Request) (*http.Response, error) {
		guggyURL := "https://text2gif.guggy.com/guggify"
		if req.URL.String() != guggyURL {
			t.Fatalf("Bad URL: got %s want %s", req.URL.String(), guggyURL)
		}
		if req.Method != "POST" {
			t.Fatalf("Bad method: got %s want POST", req.Method)
		}
		if req.Header.Get("apiKey") != apiKey {
			t.Fatalf("Bad apiKey: got %s want %s", req.Header.Get("apiKey"), apiKey)
		}
		// check the query is in the request body
		var reqBody guggyQuery
		if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to read request body: %s", err)
		}
		if reqBody.Sentence != "hey listen!" {
			t.Fatalf("Bad query: got '%s' want '%s'", reqBody.Sentence, "hey listen!")
		}

		res := guggyGifResult{
			Width:  64,
			Height: 64,
			ReqID:  "12345",
			GIF:    guggyImageURL,
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("Failed to marshal guggy response", err)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
		}, nil
	})
	// clobber the guggy service http client instance
	httpClient = &http.Client{Transport: guggyTrans}

	// Create the Guggy service
	srv, err := types.CreateService("id", ServiceType, "@guggybot:hyrule", []byte(
		`{"api_key":"`+apiKey+`"}`,
	))
	if err != nil {
		t.Fatal("Failed to create Guggy service: ", err)
	}
	guggy := srv.(*Service)

	// Mock the response from Matrix
	matrixTrans := struct{ testutils.MockTransport }{}
	matrixTrans.RT = func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == guggyImageURL { // getting the guggy image
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
	matrixCli, _ := gomatrix.NewClient("https://hyrule", "@guggybot:hyrule", "its_a_secret")
	matrixCli.Client = &http.Client{Transport: matrixTrans}

	// Execute the matrix !command
	cmds := guggy.Commands(matrixCli)
	if len(cmds) != 1 {
		t.Fatalf("Unexpected number of commands: %d", len(cmds))
	}
	cmd := cmds[0]
	_, err = cmd.Command("!someroom:hyrule", "@navi:hyrule", []string{"hey", "listen!"})
	if err != nil {
		t.Fatalf("Failed to process command: ", err.Error())
	}
}
