package native

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func jsonLines(path string, visit func(json.RawMessage) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 128*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) > 0 {
			if err := visit(json.RawMessage(line)); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func filesUnder(root string, match func(string) bool) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return nil
			}
			return err
		}
		if !entry.IsDir() && match(path) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}
