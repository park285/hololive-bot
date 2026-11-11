package scheduleposter

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const maxImageDimension = 4096

// toJPEG은 모델 출력을 Iris 발송과 Console 표시가 함께 받는 JPEG로 바꾼다. 4K 출력도 저장 상한 안에 들도록 품질을 낮춰 다시 시도한다.
func toJPEG(translated source) ([]byte, error) {
	decodeConfig, decode, ok := imageDecoders(translated.MIME)
	if !ok {
		return nil, fmt.Errorf("translated image type %q is not supported", translated.MIME)
	}

	config, err := decodeConfig(bytes.NewReader(translated.Data))
	if err != nil {
		return nil, fmt.Errorf("decode translated image header: %w", err)
	}

	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImageDimension || config.Height > maxImageDimension {
		return nil, fmt.Errorf("translated image dimensions %dx%d are out of range", config.Width, config.Height)
	}

	decoded, err := decode(bytes.NewReader(translated.Data))
	if err != nil {
		return nil, fmt.Errorf("decode translated image: %w", err)
	}

	for _, quality := range []int{90, 75} {
		var buffer bytes.Buffer

		if err := jpeg.Encode(&buffer, decoded, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("encode translated image: %w", err)
		}

		if buffer.Len() <= posterstore.MaxImageBytes {
			return buffer.Bytes(), nil
		}
	}

	return nil, errors.New("translated image exceeds the storage limit")
}

func imageDecoders(mimeType string) (func(io.Reader) (image.Config, error), func(io.Reader) (image.Image, error), bool) {
	switch mimeType {
	case "image/png":
		return png.DecodeConfig, png.Decode, true
	case "image/jpeg":
		return jpeg.DecodeConfig, jpeg.Decode, true
	default:
		return nil, nil, false
	}
}
