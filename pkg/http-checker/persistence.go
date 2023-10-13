package httpchecker

import (
	"encoding/csv"
	"os"
)

func getCsvWriter(persist bool, filename string, url string) (*csv.Writer, error) {
	var w *csv.Writer
	if !persist {
		return w, nil
	}
	f, err := os.Create(filename + "_" + url)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w = csv.NewWriter(f)
	_ = w.Write([]string{"time", "code", "latencyMillis", "ips"})
	return w, nil
}
