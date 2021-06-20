package visualize

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
)

func init() {
	http.Handle("/visualize/api/received_bytes", convreq.Wrap(renderReceivedBytes))
}

type TransferGraph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	Name string `json:"name"`
}

type Edge struct {
	Sender         string  `json:"sender"`
	Receiver       string  `json:"receiver"`
	Mode           string  `json:"mode"`
	BytesPerSecond float64 `json:"bytes_per_second"`
}

type edgedef struct {
	sender   string
	receiver string
	mode     string
}

const maxHistory = 30

var (
	mtx        sync.Mutex
	timeseries [maxHistory]map[edgedef]int64
	lastUpdate = time.Now().Unix()
)

func IncreaseReceivedBytes(sender, receiver, mode string, bytes int64) {
	mtx.Lock()
	defer mtx.Unlock()
	b := getBucket(time.Now().Unix())
	b[edgedef{sender, receiver, mode}] += bytes
}

func getBucket(now int64) map[edgedef]int64 {
	b := now % maxHistory
	if now != lastUpdate {
		forget := min(now-lastUpdate, maxHistory)
		for i := int64(1); forget > i; i++ {
			timeseries[(lastUpdate+i)%maxHistory] = nil
		}
		timeseries[b] = map[edgedef]int64{}
		lastUpdate = now
	}
	return timeseries[b]
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func historical(seconds int) map[edgedef]int64 {
	mtx.Lock()
	defer mtx.Unlock()
	now := time.Now().Unix()
	getBucket(now) // Clears old buckets if we haven't had a write in a while.
	ret := map[edgedef]int64{}
	for b := now - int64(seconds); now > b; b++ {
		for k, v := range timeseries[b%maxHistory] {
			ret[k] += v
		}
	}
	return ret
}

func toGraph(seconds int) TransferGraph {
	ret := TransferGraph{
		Nodes: []Node{},
		Edges: []Edge{},
	}
	nodes := map[string]struct{}{}
	for edge, v := range historical(seconds) {
		ret.Edges = append(ret.Edges, Edge{
			Sender:         edge.sender,
			Receiver:       edge.receiver,
			Mode:           edge.mode,
			BytesPerSecond: float64(v) / float64(seconds),
		})
		nodes[edge.sender] = struct{}{}
		nodes[edge.receiver] = struct{}{}
	}
	for n := range nodes {
		ret.Nodes = append(ret.Nodes, Node{
			Name: n,
		})
	}
	return ret
}

type receivedBytesGet struct {
	Window int
}

func renderReceivedBytes(get receivedBytesGet) convreq.HttpResponse {
	if get.Window == 0 {
		get.Window = maxHistory
	}
	if get.Window < 0 || get.Window > maxHistory {
		return respond.BadRequest("invalid value for window")
	}
	b, err := json.Marshal(toGraph(get.Window))
	if err != nil {
		return respond.Error(fmt.Errorf("JSON encode failed: %v", err))
	}
	return respond.WithHeader(respond.WithHeader(respond.Bytes(b), "Content-Type", "text/json"), "Access-Control-Allow-Origin", "*")
}
