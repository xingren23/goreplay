package main

import (
	"bytes"
	"encoding/json"
	"github.com/toolkits/pkg/logger"
	"io/ioutil"
	"net/http"
	url2 "net/url"
	"time"
)

type HeartBeat struct {
	client *HTTPClient
	url    string
}

type StatObj struct {
	Host    string            `json:"host"`
	Port    int               `json:"port"`
	AppCode string            `json:"app_code"`
	Version string            `json:"version"`
	Stats   map[string]string `json:"stats"`
}

func NewHeartBeat(url string) *HeartBeat {
	if _, err := url2.Parse(url); err != nil {
		return nil
	}
	heartbeat := &HeartBeat{}
	heartbeat.url = url
	heartbeat.client = new(HTTPClient)
	heartbeat.client.Client = &http.Client{
		Timeout: 3 * time.Second,
	}
	return heartbeat
}

// report heartbeat stats
func (h *HeartBeat) reportStat(stat StatObj) error {

	b, err := json.Marshal(stat)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", h.url, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Status == "200" {
		body, _ := ioutil.ReadAll(resp.Body)
		logger.Debug("response Body:", string(body))
	}
	return nil
}
