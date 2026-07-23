package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Deployment struct {
	Tag    string    `json:"tag"`
	Time   time.Time `json:"time"`
	Status string    `json:"status"` // success | failed
}

const historyFile = ".deployments"

func recordDeployment(dir, tag, status string) error {
	f, err := os.OpenFile(filepath.Join(dir, historyFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(Deployment{Tag: tag, Time: time.Now().UTC(), Status: status})
}

func readHistory(dir string) ([]Deployment, error) {
	f, err := os.Open(filepath.Join(dir, historyFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Deployment
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var d Deployment
		if json.Unmarshal(sc.Bytes(), &d) == nil {
			out = append(out, d)
		}
	}
	return out, sc.Err()
}

// previousSuccess returns the most recent successful tag different from current.
func previousSuccess(dir, current string) (string, bool) {
	hist, err := readHistory(dir)
	if err != nil {
		return "", false
	}
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Status == "success" && hist[i].Tag != current {
			return hist[i].Tag, true
		}
	}
	return "", false
}
