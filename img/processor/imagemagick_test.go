package processor_test

import (
	"fmt"
	"github.com/Pixboost/transformimgs/v8/img"
	"github.com/Pixboost/transformimgs/v8/img/processor"
	"io/ioutil"
	"os"
	"reflect"
	"testing"
)

type test struct {
	file                   string
	expectedOutputMimeType string
}

type result struct {
	file     string
	origSize int
	optSize  int
}

type transform func(orig []byte, imgId string) (*img.Image, error)

var (
	proc         *processor.ImageMagick
	procWithArgs *processor.ImageMagick
)

func TestMain(m *testing.M) {
	var err error

	proc, err = processor.NewImageMagick(os.ExpandEnv("${IM_HOME}/convert"), os.ExpandEnv("${IM_HOME}/identify"))
	if err != nil {
		fmt.Printf("Error while creating image processor: %+v", err)
		os.Exit(1)
	}

	procWithArgs, err = processor.NewImageMagick(os.ExpandEnv("${IM_HOME}/convert"), os.ExpandEnv("${IM_HOME}/identify"))
	if err != nil {
		fmt.Printf("Error while creating image processor: %+v", err)
		os.Exit(2)
	}
	procWithArgs.AdditionalArgs = []string{
		"-limit", "memory", "64MiB",
		"-limit", "map", "128MiB",
	}
	os.Exit(m.Run())
}

func BenchmarkImageMagickProcessor_Optimise(b *testing.B) {
	benchmarkWithFormats(b, []string{})
}

func BenchmarkImageMagickProcessor_Optimise_Webp(b *testing.B) {
	benchmarkWithFormats(b, []string{"image/webp"})
}

func BenchmarkImageMagickProcessor_Optimise_Avif(b *testing.B) {
	benchmarkWithFormats(b, []string{"image/avif"})
}

func benchmarkWithFormats(b *testing.B, formats []string) {
	f := fmt.Sprintf("%s/%s", "./test_files", "medium-jpeg.jpg")

	orig, err := ioutil.ReadFile(f)
	if err != nil {
		b.Errorf("Can't read file %s: %+v", f, err)
	}
	processor.Debug = false

	for i := 0; i < b.N; i++ {
		_, err = proc.Optimise(&img.TransformationConfig{
			Src: &img.Image{
				Id:   f,
				Data: orig,
			},
			SupportedFormats: formats,
			Quality:          0,
			Config:           nil,
		})
		if err != nil {
			b.Errorf("Can't transform file: %+v", err)
		}
	}

	processor.Debug = true
}

func TestImageMagick_GetAdditionalArgs(t *testing.T) {
	var (
		aOp     string
		aImage  []byte
		aSource *img.Info
		aTarget *img.Info
	)
	proc.GetAdditionalArgs = func(op string, image []byte, source *img.Info, target *img.Info) []string {
		aOp = op
		aImage = image
		aSource = source
		aTarget = target
		return []string{}
	}

	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.Resize(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{},
			Config:           &img.ResizeConfig{Size: "50"},
		})
	}, []*test{{"opaque-png.png", ""}})

	if aOp != "resize" {
		t.Errorf("Expected op to be resize, but got [%s]", aOp)
	}
	if len(aImage) != 201318 {
		t.Errorf("Expected source image to be 201318 bytes, but got [%d]", len(aImage))
	}
	if !reflect.DeepEqual(aSource, &img.Info{
		Format:  "PNG",
		Quality: 92,
		Opaque:  true,
		Width:   400,
		Height:  400,
		Size:    201318,
	}) {
		t.Errorf("Source image error: %+v", aSource)
	}
	if !reflect.DeepEqual(aTarget, &img.Info{
		Format:  "",
		Quality: 0,
		Opaque:  true,
		Width:   50,
		Height:  50,
		Size:    0,
	}) {
		t.Errorf("Target image error: %+v", aTarget)
	}

	proc.GetAdditionalArgs = nil
}

func TestImageMagickProcessor_NoAccept(t *testing.T) {
	tests := []*test{
		{"big-jpeg.jpg", ""},
		{"opaque-png.png", ""},
		{"transparent-png-use-original.png", ""},
		{"small-transparent-png.png", ""},
		{"animated.gif", ""},
	}

	t.Run("optimise", func(t *testing.T) {
		testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
			return proc.Optimise(&img.TransformationConfig{
				Src: &img.Image{
					Id:   imgId,
					Data: orig,
				},
				SupportedFormats: []string{},
			})
		}, tests)

		testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
			return procWithArgs.Optimise(&img.TransformationConfig{
				Src: &img.Image{
					Id:   imgId,
					Data: orig,
				},
				SupportedFormats: []string{},
			})
		}, tests)
	})

	t.Run("resize", func(t *testing.T) {
		testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
			return proc.Resize(&img.TransformationConfig{
				Src: &img.Image{
					Id:   imgId,
					Data: orig,
				},
				SupportedFormats: []string{},
				Config:           &img.ResizeConfig{Size: "50"},
			})
		}, tests)

		testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
			return procWithArgs.Resize(&img.TransformationConfig{
				Src: &img.Image{
					Id:   imgId,
					Data: orig,
				},
				SupportedFormats: []string{},
				Config:           &img.ResizeConfig{Size: "50"},
			})
		}, tests)
	})

	t.Run("fit", func(t *testing.T) {
		testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
			return proc.FitToSize(&img.TransformationConfig{
				Src: &img.Image{
					Id:   imgId,
					Data: orig,
				},
				SupportedFormats: []string{},
				Config:           &img.ResizeConfig{Size: "50x50"},
			})
		}, tests)

		testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
			return procWithArgs.FitToSize(&img.TransformationConfig{
				Src: &img.Image{
					Id:   imgId,
					Data: orig,
				},
				SupportedFormats: []string{},
				Config:           &img.ResizeConfig{Size: "50x50"},
			})
		}, tests)
	})
}

func TestImageMagickProcessor_Optimise_Webp(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.Optimise(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/webp"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", "image/webp"},
			{"opaque-png.png", "image/webp"},
			{"transparent-png-use-original.png", "image/webp"},
			{"animated.gif", "image/webp"},
			{"webp-invalid-height.jpg", ""},
		})
}

func TestImageMagickProcessor_Resize_Webp(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.Resize(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/webp"},
			Config:           &img.ResizeConfig{Size: "50"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", "image/webp"},
			{"opaque-png.png", "image/webp"},
			{"transparent-png-use-original.png", "image/webp"},
			{"animated.gif", "image/webp"},
			{"webp-invalid-height.jpg", ""},
		})
}

func TestImageMagickProcessor_FitToSize_Webp(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.FitToSize(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/webp"},
			Config:           &img.ResizeConfig{Size: "50x50"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", "image/webp"},
			{"opaque-png.png", "image/webp"},
			{"transparent-png-use-original.png", "image/webp"},
			{"animated.gif", "image/webp"},
			{"webp-invalid-height.jpg", ""},
		})
}

func TestImageMagickProcessor_Optimise_Avif(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.Optimise(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/avif"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", ""},
			{"medium-jpeg.jpg", "image/avif"},
			{"opaque-png.png", ""},
			{"animated.gif", ""},
			{"transparent-png.png", ""},
			{"small-transparent-png.png", ""},
			{"transparent-png-use-original.png", ""},
		})
}

func TestImageMagickProcessor_Optimise_Avif_Webp(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.Optimise(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/avif", "image/webp"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", "image/webp"},
			{"medium-jpeg.jpg", "image/avif"},
			{"opaque-png.png", "image/webp"},
			{"animated.gif", "image/webp"},
			{"transparent-png.png", "image/webp"},
			{"small-transparent-png.png", "image/webp"},
			{"transparent-png-use-original.png", "image/webp"},
		})
}

func TestImageMagickProcessor_Resize_Avif(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.Resize(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/avif"},
			Config:           &img.ResizeConfig{Size: "50"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", "image/avif"},
			{"medium-jpeg.jpg", "image/avif"},
			{"opaque-png.png", ""},
			{"animated.gif", ""},
			{"transparent-png-use-original.png", ""},
		})
}

func TestImageMagickProcessor_FitToSize_Avif(t *testing.T) {
	testImages(t, func(orig []byte, imgId string) (*img.Image, error) {
		return proc.FitToSize(&img.TransformationConfig{
			Src: &img.Image{
				Id:   imgId,
				Data: orig,
			},
			SupportedFormats: []string{"image/avif"},
			Config:           &img.ResizeConfig{Size: "50x50"},
		})
	},
		[]*test{
			{"big-jpeg.jpg", "image/avif"},
			{"medium-jpeg.jpg", "image/avif"},
			{"opaque-png.png", ""},
			{"animated.gif", ""},
			{"transparent-png-use-original.png", ""},
		})
}

func testImages(t *testing.T, fn transform, files []*test) {
	results := make([]*result, 0)
	for _, tt := range files {
		imgFile := tt.file

		f := fmt.Sprintf("%s/%s", "./test_files", imgFile)

		orig, err := ioutil.ReadFile(f)
		if err != nil {
			t.Errorf("Can't read file %s: %+v", f, err)
		}

		transformedImg, err := fn(orig, f)

		if err != nil {
			t.Errorf("Can't transform file: %+v", err)
		}

		results = append(results, &result{
			file:     imgFile,
			origSize: len(orig),
			optSize:  len(transformedImg.Data),
		})
		//Writes converted file for manual verification.
		// ioutil.WriteFile(fmt.Sprintf("./test_files/opt_%s_%s", t.Name(), imgFile), transformedImg, 0777)

		if transformedImg.MimeType != tt.expectedOutputMimeType {
			t.Errorf("%s: Expected [%s] mime type, but got [%s]", tt.file, tt.expectedOutputMimeType, transformedImg.MimeType)
		}

		if len(transformedImg.Data) > len(orig) {
			t.Errorf("Image %s is not optimised", f)
		}
	}

	for _, r := range results {
		fmt.Printf("%60s | %10d | %10d | %.2f\n", r.file, r.optSize, r.origSize, 1.0-(float32(r.optSize)/float32(r.origSize)))
	}
}
