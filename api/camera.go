package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func (c *Client) CameraTrackNames() ([]string, error) {
	var result []string
	if err := c.doRequest("/camera/tracknames", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Snapshot(trackName string) ([]byte, error) {
	u := c.baseURL
	u.Path = "/camera/snapshot"
	u.RawQuery = "track=" + url.QueryEscape(trackName)

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var fullOut struct {
			Error *string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&fullOut); err != nil {
			return nil, err
		}
		if fullOut.Error != nil {
			return nil, &RemoteError{Message: *fullOut.Error}
		}
		return nil, fmt.Errorf("unexpected status code from camera snapshot: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
