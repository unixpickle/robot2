package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Recording struct {
	parentDir string
	outputDir string
}

func NewRecording(parentDir string) (*Recording, error) {
	ts := time.Now().Local()
	outDir := filepath.Join(
		parentDir,
		"all",
		fmt.Sprintf("%04d-%02d-%02d-%02d%02d%02d", ts.Year(), ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second()),
	)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, err
	}
	return &Recording{
		parentDir: parentDir,
		outputDir: outDir,
	}, nil
}

func (r *Recording) VideoPath(trackName string) string {
	return filepath.Join(r.outputDir, trackName+".mp4")
}

func (r *Recording) WriteImage(trackName string, data []byte) error {
	path := filepath.Join(r.outputDir, trackName+".jpg")
	return os.WriteFile(path, data, 0644)
}

func (r *Recording) WriteJSON(dataName string, obj any) error {
	path := filepath.Join(r.outputDir, dataName+".json")
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (r *Recording) MarkSuccessful(success bool) error {
	label := "success"
	if !success {
		label = "failure"
	}
	baseName := filepath.Base(r.outputDir)
	if err := os.MkdirAll(filepath.Join(r.parentDir, label), 0755); err != nil {
		return err
	}
	outPath := filepath.Join(r.parentDir, label, baseName)
	return os.Symlink(filepath.Join("..", "all", baseName), outPath)
}
