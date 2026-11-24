//go:build !darwin && !windows

package main

func newTextRecognizer() (textRecognizer, error) {
	return nil, errNoOCR
}
