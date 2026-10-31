package devnet

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Status is one validator's committed height and application hash.
type Status struct {
	Height  int64
	AppHash string
}

// WaitEqual waits until every RPC reports the same application hash at a
// height of at least minHeight.
func WaitEqual(rpcs []string, minHeight int64, timeout time.Duration) (int64, string, error) {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		height, hash, err := equal(rpcs, minHeight)
		if err == nil {
			return height, hash, nil
		}
		last = err
		time.Sleep(300 * time.Millisecond)
	}
	return 0, "", fmt.Errorf("validators did not agree within %s: %v", timeout, last)
}

// WaitAbove waits until every RPC is above height.
func WaitAbove(rpcs []string, height int64, timeout time.Duration) (int64, string, error) {
	return WaitEqual(rpcs, height+1, timeout)
}

// Stable reports whether every RPC stays at the same height for the duration.
func Stable(rpcs []string, duration time.Duration) (int64, error) {
	first, err := readAll(rpcs)
	if err != nil {
		return 0, err
	}
	height := first[0].Height
	for _, item := range first[1:] {
		if item.Height != height {
			return 0, fmt.Errorf("heights already differ")
		}
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		current, err := readAll(rpcs)
		if err != nil {
			return height, err
		}
		for _, item := range current {
			if item.Height != height {
				return item.Height, fmt.Errorf("height moved from %d to %d", height, item.Height)
			}
		}
	}
	return height, nil
}

// BlockHash reads the committed application hash at one height.
func BlockHash(rpc string, height int64) (string, error) {
	body, err := get(fmt.Sprintf("%s/block?height=%d", strings.TrimRight(rpc, "/"), height))
	if err != nil {
		return "", err
	}
	var parsed struct {
		Result struct {
			Block struct {
				Header struct {
					AppHash string `json:"app_hash"`
				} `json:"header"`
			} `json:"block"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Result.Block.Header.AppHash == "" {
		return "", fmt.Errorf("block %d has no app hash", height)
	}
	return strings.ToUpper(parsed.Result.Block.Header.AppHash), nil
}

func equal(rpcs []string, minHeight int64) (int64, string, error) {
	rows, err := readAll(rpcs)
	if err != nil {
		return 0, "", err
	}
	height := rows[0].Height
	hash := rows[0].AppHash
	if height < minHeight {
		return 0, "", fmt.Errorf("height %d is below %d", height, minHeight)
	}
	for _, row := range rows[1:] {
		if row.Height != height || !strings.EqualFold(row.AppHash, hash) {
			return 0, "", fmt.Errorf("disagreement height %d/%d hash %s/%s", height, row.Height, hash, row.AppHash)
		}
	}
	return height, strings.ToUpper(hash), nil
}

func readAll(rpcs []string) ([]Status, error) {
	rows := make([]Status, len(rpcs))
	for i, rpc := range rpcs {
		status, err := ReadStatus(rpc)
		if err != nil {
			return nil, err
		}
		rows[i] = status
	}
	return rows, nil
}

// ReadStatus queries one CometBFT RPC status endpoint.
func ReadStatus(rpc string) (Status, error) {
	body, err := get(strings.TrimRight(rpc, "/") + "/status")
	if err != nil {
		return Status{}, err
	}
	var parsed struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight flexInt `json:"latest_block_height"`
				LatestAppHash     string  `json:"latest_app_hash"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Status{}, err
	}
	if parsed.Result.SyncInfo.LatestBlockHeight == 0 {
		return Status{}, fmt.Errorf("no committed block yet")
	}
	return Status{
		Height:  int64(parsed.Result.SyncInfo.LatestBlockHeight),
		AppHash: strings.ToUpper(parsed.Result.SyncInfo.LatestAppHash),
	}, nil
}

type flexInt int64

func (n *flexInt) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(string(raw), `"`)
	if text == "" || text == "null" {
		*n = 0
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	*n = flexInt(value)
	return nil
}

func get(url string) ([]byte, error) {
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, response.Status)
	}
	return body, nil
}
