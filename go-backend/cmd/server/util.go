package main

import (
	"fmt"
	"io"
	"net/http"
)

func readAllLimit(resp *http.Response, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件过大")
	}
	return data, nil
}
