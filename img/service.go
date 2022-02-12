package img

import (
	"context"
	"fmt"
	"github.com/dooman87/glogi"
	"github.com/gorilla/mux"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Number of seconds that will be written to max-age HTTP header
var CacheTTL int

// SaveDataEnabled is flag to enable/disable Save-Data client hint.
// Sometime CDN doesn't support Save-Data in Vary response header in which
// case you would need to set this to false
var SaveDataEnabled bool = true

// Log is a logger that could be overridden. Should implement interface glogi.Logger.
// By default is using glogi.SimpleLogger.
var Log glogi.Logger = glogi.NewSimpleLogger()

// Loader is responsible for loading an original image for transformation
type Loader interface {
	// Load loads an image from the given source.
	//
	// ctx is a context of the current transaction. Typically it's a context
	// of an incoming HTTP request, so we make it possible to pass values through middlewares.
	//
	// Returns an image.
	Load(src string, ctx context.Context) (*Image, error)
}

type Quality int

const (
	DEFAULT Quality = 1 + iota
	LOW
)

type ResizeConfig struct {
	// Size is a size of output images in the format WxH.
	Size string
}

// TransformationConfig is a configuration passed to Processor
// that used during transformations.
type TransformationConfig struct {
	// Src is a source image to transform.
	// This field is required for transformations.
	Src *Image
	// SupportedFormats is a list of output formats supported by client.
	// Processor will use one of those formats for result image. If list
	// is empty the format of the source image will be used.
	SupportedFormats []string
	// Quality defines quality of output image
	Quality Quality
	// Config is a configuration for the specific transformation
	Config interface{}
}

// Processor is an interface for transforming/optimising images.
//
// Each function accepts original image and a list of supported
// output format by client. Each format should be a MIME type, e.g.
// image/png, image/webp. The output image will be encoded in one
// of those formats.
type Processor interface {
	// Resize resizes given image preserving aspect ratio.
	// Format of the the size argument is width'x'height.
	// Any dimension could be skipped.
	// For example:
	//* 300x200
	//* 300 - only width
	//* x200 - only height
	Resize(input *TransformationConfig) (*Image, error)

	// FitToSize resizes given image cropping it to the given size and does not respect aspect ratio.
	// Format of the the size string is width'x'height, e.g. 300x400.
	FitToSize(input *TransformationConfig) (*Image, error)

	// Optimise optimises given image to reduce size of the served image.
	Optimise(input *TransformationConfig) (*Image, error)
}

type Service struct {
	Loader      Loader
	Processor   Processor
	Q           []*Queue
	currProc    int
	currProcMux sync.Mutex
}

type Cmd func(input *TransformationConfig) (*Image, error)

type Command struct {
	Transformation Cmd
	Config         *TransformationConfig
	Resp           http.ResponseWriter
	Result         *Image
	FinishedCond   *sync.Cond
	Finished       bool
	Err            error
}

var emptyGif = [...]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x1, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x21, 0xf9, 0x4, 0x1, 0xa, 0x0, 0x1, 0x0, 0x2c, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x1, 0x0, 0x0, 0x2, 0x2, 0x4c, 0x1, 0x0, 0x3b}

func NewService(r Loader, p Processor, procNum int) (*Service, error) {
	if procNum <= 0 {
		return nil, fmt.Errorf("procNum must be positive, but got [%d]", procNum)
	}

	Log.Printf("Creating new service with [%d] number of processors\n", procNum)

	srv := &Service{
		Loader:    r,
		Processor: p,
		Q:         make([]*Queue, procNum),
	}

	for i := 0; i < procNum; i++ {
		srv.Q[i] = NewQueue()
	}
	srv.currProc = 0

	return srv, nil
}

func (r *Service) GetRouter() *mux.Router {
	router := mux.NewRouter().SkipClean(true)
	router.HandleFunc("/img/{imgUrl:.*}/resize", r.ResizeUrl)
	router.HandleFunc("/img/{imgUrl:.*}/fit", r.FitToSizeUrl)
	router.HandleFunc("/img/{imgUrl:.*}/asis", r.AsIs)
	router.HandleFunc("/img/{imgUrl:.*}/optimise", r.OptimiseUrl)

	return router
}

// swagger:operation GET /img/{imgUrl}/optimise optimiseImage
//
// Optimises image from the given url.
//
// ---
// tags:
// - images
// produces:
// - image/png
// - image/jpeg
// - image/webp
// - image/avif
// parameters:
// - name: imgUrl
//   required: true
//   in: path
//   type: string
//   description: >
//     Url of the original image including schema. Note that query parameters
//     need to be properly encoded
//   examples:
//     simple:
//       value: https://yoursite.com/image.png
//     with-query-params:
//       value: https://yoursite.com/image.png%3Fv%3D123
//       summary: URL with encoded query parameters, replaced ? with %3F, and = with %3D
// - name: save-data
//   required: false
//   in: query
//   type: string
//   enum: ["off", hide]
//   description: >
//     Sets an optional behaviour when Save-Data header is "on".
//     When passing "off" value the result image won't use extra
//     compression when data saver mode is on.
//     When passing "hide" value the result image will be an empty 1x1 image.
//     When absent the API will use reduced quality for result images.
// - name: dppx
//   required: false
//   default: 1
//   in: query
//   type: number
//   format: float
//   description: >
//     Number of dots per pixel defines the ratio between device and CSS pixels.
//     The query parameter is a hint that enables extra optimisations for high
//     density screens. The format is a float number that's the same format as window.devicePixelRatio.
//   examples:
//     desktop:
//       value: 1
//       summary: Most desktop monitors
//     iphonese:
//       value: 2
//       summary: IPhone SE
//     galaxy51:
//       value: 2.625
//       summary: Samsung Galaxy A51
//     galaxy8:
//       value: 4
//       summary: Samsung Galaxy S8
// responses:
//   '200':
//     description: Optimised image.
func (r *Service) OptimiseUrl(resp http.ResponseWriter, req *http.Request) {
	r.transformUrl(resp, req, r.Processor.Optimise, nil)
}

// swagger:operation GET /img/{imgUrl}/resize resizeImage
//
// Resizes, optimises image and preserve aspect ratio.
// Use /fit operation for resizing to the exact size.
//
// ---
// tags:
// - images
// produces:
// - image/png
// - image/jpeg
// - image/webp
// - image/avif
// parameters:
// - name: imgUrl
//   required: true
//   in: path
//   type: string
//   description: >
//     Url of the original image including schema. Note that query parameters
//     need to be properly encoded
//   examples:
//     simple:
//       value: https://yoursite.com/image.png
//     with-query-params:
//       value: https://yoursite.com/image.png%3Fv%3D123
//       summary: URL with encoded query parameters, replaced ? with %3F, and = with %3D
// - name: size
//   required: true
//   in: query
//   type: string
//   description: |
//    Size of the result image. Should be in the format 'width'x'height', e.g. 200x300
//    Only width or height could be passed, e.g 200, x300.
//   examples:
//     width-and-height:
//       value: 200x300
//     only-width:
//       value: 200
//     only-height:
//       value: 300
// - name: save-data
//   required: false
//   in: query
//   type: string
//   enum: ["off", hide]
//   description: >
//     Sets an optional behaviour when Save-Data header is "on".
//     When passing "off" value the result image won't use extra
//     compression when data saver mode is on.
//     When passing "hide" value the result image will be an empty 1x1 image.
//     When absent the API will use reduced quality for result images.
// - name: dppx
//   required: false
//   default: 1
//   in: query
//   type: number
//   format: float
//   description: >
//     Number of dots per pixel defines the ratio between device and CSS pixels.
//     The query parameter is a hint that enables extra optimisations for high
//     density screens. The format is a float number that's the same format as window.devicePixelRatio.
//   examples:
//     desktop:
//       value: 1
//       summary: Most desktop monitors
//     iphonese:
//       value: 2
//       summary: IPhone SE
//     galaxy51:
//       value: 2.625
//       summary: Samsung Galaxy A51
//     galaxy8:
//       value: 4
//       summary: Samsung Galaxy S8
// responses:
//   '200':
//     description: Resized image.
func (r *Service) ResizeUrl(resp http.ResponseWriter, req *http.Request) {

	size := getQueryParam(req.URL, "size")
	if len(size) == 0 {
		http.Error(resp, "size param is required", http.StatusBadRequest)
		return
	}
	if match, err := regexp.MatchString(`^\d*[x]?\d*$`, size); !match || err != nil {
		if err != nil {
			Log.Printf("Error while matching size: %s\n", err.Error())
		}
		http.Error(resp, "size param should be in format WxH", http.StatusBadRequest)
		return
	}

	r.transformUrl(resp, req, r.Processor.Resize, &ResizeConfig{Size: size})
}

// swagger:operation GET /img/{imgUrl}/fit fitImage
//
// Resizes, crops, and optimises an image to the exact size.
// If you need to resize image with preserved aspect ratio then use /resize endpoint.
//
// ---
// tags:
// - images
// produces:
// - image/png
// - image/jpeg
// - image/webp
// - image/avif
// parameters:
// - name: imgUrl
//   required: true
//   in: path
//   type: string
//   description: >
//     Url of the original image including schema. Note that query parameters
//     need to be properly encoded
//   examples:
//     simple:
//       value: https://yoursite.com/image.png
//     with-query-params:
//       value: https://yoursite.com/image.png%3Fv%3D123
//       summary: URL with encoded query parameters, replaced ? with %3F, and = with %3D
// - name: size
//   required: true
//   in: query
//   type: string
//   pattern: \d{1,4}x\d{1,4}
//   description: >
//    size of the image in the response. Should be in the format 'width'x'height', e.g. 200x300
//   examples:
//     size:
//       value: 200x300
// - name: save-data
//   required: false
//   in: query
//   type: string
//   enum: ["off", hide]
//   description: >
//     Sets an optional behaviour when Save-Data header is "on".
//     When passing "off" value the result image won't use extra
//     compression when data saver mode is on.
//     When passing "hide" value the result image will be an empty 1x1 image.
//     When absent the API will use reduced quality for result images.
// - name: dppx
//   required: false
//   default: 1
//   in: query
//   type: number
//   format: float
//   description: >
//     Number of dots per pixel defines the ratio between device and CSS pixels.
//     The query parameter is a hint that enables extra optimisations for high
//     density screens. The format is a float number that's the same format as window.devicePixelRatio.
//   examples:
//     desktop:
//       value: 1
//       summary: Most desktop monitors
//     iphonese:
//       value: 2
//       summary: IPhone SE
//     galaxy51:
//       value: 2.625
//       summary: Samsung Galaxy A51
//     galaxy8:
//       value: 4
//       summary: Samsung Galaxy S8
// responses:
//   '200':
//     description: Resized image
func (r *Service) FitToSizeUrl(resp http.ResponseWriter, req *http.Request) {
	size := getQueryParam(req.URL, "size")
	if len(size) == 0 {
		http.Error(resp, "size param is required", http.StatusBadRequest)
		return
	}
	if match, err := regexp.MatchString(`^\d*[x]\d*$`, size); !match || err != nil {
		if err != nil {
			Log.Printf("Error while matching size: %s\n", err.Error())
		}
		http.Error(resp, "size param should be in format WxH", http.StatusBadRequest)
		return
	}

	r.transformUrl(resp, req, r.Processor.FitToSize, &ResizeConfig{Size: size})
}

// swagger:operation GET /img/{imgUrl}/asis asisImage
//
// Respond with original image without any modifications
//
// ---
// tags:
// - images
// produces:
// - image/png
// - image/jpeg
// parameters:
// - name: imgUrl
//   required: true
//   in: path
//   type: string
//   description: >
//     Url of the original image including schema. Note that query parameters
//     need to be properly encoded
//   examples:
//     simple:
//       value: https://yoursite.com/image.png
//     with-query-params:
//       value: https://yoursite.com/image.png%3Fv%3D123
//       summary: URL with encoded query parameters, replaced ? with %3F, and = with %3D
// responses:
//   '200':
//     description: Requested image.
func (r *Service) AsIs(resp http.ResponseWriter, req *http.Request) {
	imgUrl := getImgUrl(req)
	if len(imgUrl) == 0 {
		http.Error(resp, "url param is required", http.StatusBadRequest)
		return
	}

	Log.Printf("Requested image %s as is\n", imgUrl)

	result, err := r.Loader.Load(imgUrl, req.Context())

	if err != nil {
		http.Error(resp, fmt.Sprintf("Error reading image: '%s'", err.Error()), http.StatusInternalServerError)
		return
	} else {
		if len(result.MimeType) > 0 {
			resp.Header().Add("Content-Type", result.MimeType)
		}

		r.execOp(&Command{
			Config: &TransformationConfig{
				Src: &Image{
					Id: imgUrl,
				},
			},
			Result: result,
			Resp:   resp,
		})
	}
}

func (r *Service) execOp(op *Command) {
	op.FinishedCond = sync.NewCond(&sync.Mutex{})

	queue := r.getQueue()
	queue.AddAndWait(op, func() {
		Log.Printf("Image [%s] transformed successfully, writing to the response", op.Config.Src.Id)
		writeResult(op)
	})
}

func (r *Service) getQueue() *Queue {
	// Get the next execution channel
	r.currProcMux.Lock()
	r.currProc++
	if r.currProc == len(r.Q) {
		r.currProc = 0
	}
	procIdx := r.currProc
	r.currProcMux.Unlock()

	return r.Q[procIdx]
}

// Adds Content-Length and Cache-Control headers
func addHeaders(resp http.ResponseWriter, image *Image) {
	if len(image.MimeType) != 0 {
		resp.Header().Add("Content-Type", image.MimeType)
	}
	resp.Header().Add("Content-Length", strconv.Itoa(len(image.Data)))
	resp.Header().Add("Cache-Control", fmt.Sprintf("public, max-age=%d", CacheTTL))
}

func getQueryParam(url *url.URL, name string) string {
	if len(url.Query()[name]) == 1 {
		return url.Query()[name][0]
	}
	return ""
}

func getImgUrl(req *http.Request) string {
	imgUrl := mux.Vars(req)["imgUrl"]
	if len(imgUrl) == 0 {
		return ""
	}

	if strings.HasPrefix(imgUrl, "//") && len(req.Header["X-Forwarded-Proto"]) == 1 {
		imgUrl = fmt.Sprintf("%s:%s", req.Header["X-Forwarded-Proto"][0], imgUrl)
	}

	return imgUrl
}

func getSupportedFormats(req *http.Request) []string {
	acceptHeader := req.Header["Accept"]
	if len(acceptHeader) > 0 {
		accepts := strings.Split(acceptHeader[0], ",")
		trimmedAccepts := make([]string, len(accepts))
		for i, a := range accepts {
			trimmedAccepts[i] = strings.TrimSpace(a)
		}
		return trimmedAccepts
	}

	return []string{}
}

func writeResult(op *Command) {
	if op.Err != nil {
		http.Error(op.Resp, fmt.Sprintf("Error transforming image: '%s'", op.Err.Error()), http.StatusInternalServerError)
		return
	}

	addHeaders(op.Resp, op.Result)
	op.Resp.Write(op.Result.Data)
}

func (r *Service) transformUrl(resp http.ResponseWriter, req *http.Request, transformation Cmd, config interface{}) {
	imgUrl := getImgUrl(req)
	if len(imgUrl) == 0 {
		http.Error(resp, "url param is required", http.StatusBadRequest)
		return
	}

	Log.Printf("Transforming image %s using config %+v\n", imgUrl, config)

	if SaveDataEnabled {
		resp.Header().Add("Vary", "Accept, Save-Data")

		saveDataHeader := req.Header.Get("Save-Data")
		saveDataQueryParam := getQueryParam(req.URL, "save-data")
		if saveDataHeader == "on" && saveDataQueryParam == "hide" {
			_, _ = resp.Write(emptyGif[:])
			return
		}
	} else {
		resp.Header().Add("Vary", "Accept")
	}

	supportedFormats := getSupportedFormats(req)

	srcImage, err := r.Loader.Load(imgUrl, req.Context())
	if err != nil {
		http.Error(resp, fmt.Sprintf("Error reading image: '%s'", err.Error()), http.StatusInternalServerError)
		return
	}
	Log.Printf("Source image [%s] loaded successfully, adding to the queue\n", imgUrl)

	r.execOp(&Command{
		Transformation: transformation,
		Config: &TransformationConfig{
			Src:              srcImage,
			SupportedFormats: supportedFormats,
			Quality:          getQuality(req),
			Config:           config,
		},
		Resp: resp,
	})
}

func getQuality(req *http.Request) Quality {
	quality := DEFAULT

	if SaveDataEnabled {
		saveDataParam := getQueryParam(req.URL, "save-data")
		saveDataHeader := req.Header.Get("Save-Data")

		if saveDataHeader == "on" && saveDataParam != "off" {
			quality = LOW
		}
	}

	return quality
}
