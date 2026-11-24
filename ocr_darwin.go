//go:build darwin

package main

/*
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Foundation -framework Vision -framework ImageIO -framework CoreGraphics -mmacosx-version-min=13.0
#include <stdlib.h>
char *ocr_recognize(const char *path);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

type visionRecognizer struct{}

type visionBox struct {
	T string  `json:"t"`
	C float64 `json:"c"`
	K string  `json:"k"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type visionReply struct {
	Lines    []visionBox `json:"lines"`
	Barcodes []visionBox `json:"barcodes"`
	Faces    []visionBox `json:"faces"`
}

func newTextRecognizer() (textRecognizer, error) {
	return visionRecognizer{}, nil
}

func (visionRecognizer) recognize(path string) (ocrResult, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	raw := C.ocr_recognize(cPath)
	if raw == nil {
		return ocrResult{}, errors.New("text recognition failed")
	}
	defer C.free(unsafe.Pointer(raw))
	var parsed visionReply
	if err := json.Unmarshal([]byte(C.GoString(raw)), &parsed); err != nil {
		return ocrResult{}, err
	}
	var res ocrResult
	for _, l := range parsed.Lines {
		res.Lines = append(res.Lines, ocrLine{Text: l.T, Conf: l.C, X: l.X, Y: l.Y, W: l.W, H: l.H})
	}
	for _, b := range parsed.Barcodes {
		res.Barcodes = append(res.Barcodes, ocrBarcode{Kind: b.K, Box: ocrLine{X: b.X, Y: b.Y, W: b.W, H: b.H}})
	}
	for _, f := range parsed.Faces {
		res.Faces = append(res.Faces, ocrLine{X: f.X, Y: f.Y, W: f.W, H: f.H})
	}
	return res, nil
}

func (visionRecognizer) close() {}
