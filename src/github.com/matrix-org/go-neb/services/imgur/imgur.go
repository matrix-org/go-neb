// Package imgur implements a Service which adds !commands for imgur image search
package imgur

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// ServiceType of the Imgur service
const ServiceType = "imgur"

var httpClient = &http.Client{}

type imgurGalleryImage struct {
	ID          string `json:"id"`          // id	string	The ID for the image
	Title       string `json:"title"`       // title	string	The title of the image.
	Description string `json:"description"` // description	string	Description of the image.
	DateTime    int64  `json:"datetime"`    // datetime	integer	Time inserted into the gallery, epoch time
	Type        string `json:"type"`        // type	string	Image MIME type.
	Animated    bool   `json:"animated"`    // animated	boolean	is the image animated
	Width       int    `json:"width"`       // width	integer	The width of the image in pixels
	Height      int    `json:"height"`      // height	integer	The height of the image in pixels
	Size        int64  `json:"size"`        // size	integer	The size of the image in bytes
	Views       int64  `json:"views"`       // views	integer	The number of image views
	Link        string `json:"link"`        // link	string	The direct link to the the image. (Note: if fetching an animated GIF that was over 20MB in original size, a .gif thumbnail will be returned)
	Gifv        string `json:"gifv"`        // gifv	string	OPTIONAL, The .gifv link. Only available if the image is animated and type is 'image/gif'.
	MP4         string `json:"mp4"`         // mp4	string	OPTIONAL, The direct link to the .mp4. Only available if the image is animated and type is 'image/gif'.
	MP4Size     int64  `json:"mp4_size"`    // mp4_size	integer	OPTIONAL, The Content-Length of the .mp4. Only available if the image is animated and type is 'image/gif'. Note that a zero value (0) is possible if the video has not yet been generated
	Looping     bool   `json:"looping"`     // looping	boolean	OPTIONAL, Whether the image has a looping animation. Only available if the image is animated and type is 'image/gif'.
	NSFW        bool   `json:"nsfw"`        // nsfw	boolean	Indicates if the image has been marked as nsfw or not. Defaults to null if information is not available.
	Topic       string `json:"topic"`       // topic	string	Topic of the gallery image.
	Section     string `json:"section"`     // section	string	If the image has been categorized by our backend then this will contain the section the image belongs in. (funny, cats, adviceanimals, wtf, etc)
	IsAlbum     bool   `json:"is_album"`    // is_album	boolean	If it's an album or not
	// ** Uninplemented fields **
	// bandwidth	integer	Bandwidth consumed by the image in bytes
	// deletehash	string	OPTIONAL, the deletehash, if you're logged in as the image owner
	// comment_count	int	Number of comments on the gallery image.
	// topic_id	integer	Topic ID of the gallery image.
	// vote	string	The current user's vote on the album. null if not signed in or if the user hasn't voted on it.
	// favorite	boolean	Indicates if the current user favorited the image. Defaults to false if not signed in.
	// account_url	string	The username of the account that uploaded it, or null.
	// account_id	integer	The account ID of the account that uploaded it, or null.
	// ups	integer	Upvotes for the image
	// downs	integer	Number of downvotes for the image
	// points	integer	Upvotes minus downvotes
	// score	integer	Imgur popularity score
}

type imgurGalleryAlbum struct {
	ID          string              `json:"id"`           // id	string	The ID for the album
	Title       string              `json:"title"`        // title	string	The title of the album.
	Description string              `json:"description"`  // description	string	Description of the album.
	DateTime    int64               `json:"datetime"`     // datetime	integer	Time inserted into the gallery, epoch time
	Views       int64               `json:"views"`        // views	integer	The number of album views
	Link        string              `json:"link"`         // link	string	The URL link to the album
	NSFW        bool                `json:"nsfw"`         // nsfw	boolean	Indicates if the album has been marked as nsfw or not. Defaults to null if information is not available.
	Topic       string              `json:"topic"`        // topic	string	Topic of the gallery album.
	IsAlbum     bool                `json:"is_album"`     // is_album	boolean	If it's an album or not
	Cover       string              `json:"cover"`        // cover	string	The ID of the album cover image
	CoverWidth  int                 `json:"cover_width"`  // cover_width	integer	The width, in pixels, of the album cover image
	CoverHeight int                 `json:"cover_height"` // cover_height	integer	The height, in pixels, of the album cover image
	ImagesCount int                 `json:"images_count"` // images_count	integer	The total number of images in the album
	Images      []imgurGalleryImage `json:"images"`       // images	Array of Images	An array of all the images in the album (only available when requesting the direct album)

	// ** Unimplemented fields **
	// account_url	string	The account username or null if it's anonymous.
	// account_id	integer	The account ID of the account that uploaded it, or null.
	// privacy	string	The privacy level of the album, you can only view public if not logged in as album owner
	// layout	string	The view layout of the album.
	// views	integer	The number of image views
	// ups	integer	Upvotes for the image
	// downs	integer	Number of downvotes for the image
	// points	integer	Upvotes minus downvotes
	// score	integer	Imgur popularity score
	// vote	string	The current user's vote on the album. null if not signed in or if the user hasn't voted on it.
	// favorite	boolean	Indicates if the current user favorited the album. Defaults to false if not signed in.
	// comment_count	int	Number of comments on the gallery album.
	// topic_id	integer	Topic ID of the gallery album.
}

type imgurSearchResponse struct {
	Data    []json.RawMessage `json:"data"`
	Success bool              `json:"success"` // Request completed successfully
	Status  int               `json:"status"`  // HTTP response code
}

// Service contains the Config fields for the Imgur service.
//
// Example request:
//   {
//			"client_id": "AIzaSyA4FD39..."
//			"client_secret": "ASdsaijwdfASD..."
//   }
type Service struct {
	types.DefaultService
	// The Imgur client ID
	ClientID string `json:"client_id"`
	// The API key to use when making HTTP requests to Imgur.
	ClientSecret string `json:"client_secret"`
}

// Commands supported:
//    !imgur some_search_query_without_quotes
// Responds with a suitable image into the same room as the command.
func (s *Service) Commands(client *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"imgur", "help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return usageMessage(), nil
			},
		},
		types.Command{
			Path: []string{"imgur"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdImgurImgSearch(client, roomID, userID, args)
			},
		},
	}
}

// usageMessage returns a matrix TextMessage representation of the service usage
func usageMessage() *gomatrix.TextMessage {
	return &gomatrix.TextMessage{"m.notice",
		`Usage: !imgur image_search_text`}
}

func (s *Service) cmdImgurImgSearch(client *gomatrix.Client, roomID, userID string, args []string) (interface{}, error) {

	if len(args) < 1 {
		return usageMessage(), nil
	}

	// Get the query text to search for.
	querySentence := strings.Join(args, " ")

	searchResultImage, searchResultAlbum, err := s.text2imgImgur(querySentence)

	if err != nil {
		return nil, err
	}

	// Image returned
	if searchResultImage != nil {
		var imgURL = searchResultImage.Link
		if imgURL == "" {
			return gomatrix.TextMessage{
				MsgType: "m.notice",
				Body:    "No image found!",
			}, nil
		}

		// FIXME -- Sometimes upload fails with a cryptic error - "msg=Upload request failed code=400"
		// log.Printf("Uploading image at: %s", imgURL)
		resUpload, err := client.UploadLink(imgURL)
		if err != nil {
			return nil, fmt.Errorf("Failed to upload Imgur image (%s) to matrix: %s", imgURL, err.Error())
		}

		return gomatrix.ImageMessage{
			MsgType: "m.image",
			Body:    querySentence,
			URL:     resUpload.ContentURI,
			Info: gomatrix.ImageInfo{
				Height:   uint(searchResultImage.Height),
				Width:    uint(searchResultImage.Width),
				Mimetype: searchResultImage.Type,
			},
		}, nil
	} else if searchResultAlbum != nil {
		return gomatrix.TextMessage{
			MsgType: "m.notice",
			Body:    "Search returned an album - Not currently supported",
		}, nil
	} else {
		return gomatrix.TextMessage{
			MsgType: "m.notice",
			Body:    "No image found!",
		}, nil
	}
}

// text2imgImgur returns info about an image or an album
func (s *Service) text2imgImgur(query string) (*imgurGalleryImage, *imgurGalleryAlbum, error) {
	log.Info("Searching Imgur for an image of a ", query)

	query = url.QueryEscape(query)
	var base = "https://api.imgur.com/3/gallery/search"
	var sort = "time"  // time | viral | top
	var window = "all" // day | week | month | year | all
	var page = 1
	var urlString = fmt.Sprintf("%s/%s/%s/%d?q=%s", base, sort, window, page, query)
	// var urlString = fmt.Sprintf("%s?q=%s", base, query)

	u, err := url.Parse(urlString)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Add("Authorization", "Client-ID "+s.ClientID)
	res, err := httpClient.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("Request error: %d, %s", res.StatusCode, response2String(res))
	}

	var searchResults imgurSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&searchResults); err != nil {
		return nil, nil, fmt.Errorf("No images found - %s", err.Error())
	} else if len(searchResults.Data) < 1 {
		return nil, nil, fmt.Errorf("No images found")
	}

	log.Printf("%d results were returned from Imgur", len(searchResults.Data))
	// Return a random image result
	var images []imgurGalleryImage
	for i := 0; i < len(searchResults.Data); i++ {
		var image imgurGalleryImage
		if err := json.Unmarshal(searchResults.Data[i], &image); err == nil && !image.IsAlbum {
			images = append(images, image)
		}
	}
	if len(images) > 0 {
		var r = 0
		if len(images) > 1 {
			r = rand.Intn(len(images) - 1)
		}
		return &images[r], nil, nil
	}

	return nil, nil, fmt.Errorf("No images found")
}

// response2String returns a string representation of an HTTP response body
func response2String(res *http.Response) string {
	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "Failed to decode response body"
	}
	str := string(bs)
	return str
}

// Initialise the service
func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
