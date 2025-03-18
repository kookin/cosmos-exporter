package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Define Prometheus metrics.
var (
	serverInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "server_info",
			Help: "Static information about this Cosmos node (node ID, network, and version).",
		},
		[]string{"node_id", "network", "version"},
	)

	highestBlock = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cosmos_highest_block_number",
		Help: "Highest block number",
	})

	blockDrift = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cosmos_block_drift_seconds",
		Help: "Current block drift time in seconds (current time minus block creation time)",
	})

	peerCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cosmos_connected_peers",
		Help: "Number of connected peers",
	})

	peerVersionCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cosmos_peers_by_version",
			Help: "Number of peers grouped by their version",
		},
		[]string{"version"},
	)
)

func init() {
	prometheus.MustRegister(serverInfo)
	prometheus.MustRegister(highestBlock)
	prometheus.MustRegister(blockDrift)
	prometheus.MustRegister(peerCount)
	prometheus.MustRegister(peerVersionCount)
}

// lastBlockHeight is used to ensure we process each block only once.
var lastBlockHeight int64 = 0

// StatusResponse models the JSON returned by the /status endpoint.
type StatusResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  struct {
		NodeInfo struct {
			ID      string `json:"id"`
			Network string `json:"network"`
			Version string `json:"version"`
		} `json:"node_info"`
		SyncInfo struct {
			LatestBlockHeight string `json:"latest_block_height"`
			LatestBlockTime   string `json:"latest_block_time"`
		} `json:"sync_info"`
		Peers []struct {
			Version string `json:"version"`
		} `json:"peers"`
	} `json:"result"`
}

// BlockResultsResponse models the JSON returned by the /block_results endpoint.
type BlockResultsResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  struct {
		Height     string `json:"height"`
		TxsResults []struct {
			Code uint32 `json:"code"`
			Log  string `json:"log"`
		} `json:"txs_results"`
	} `json:"result"`
}

// updateStatusMetrics queries the /status endpoint and updates the serverInfo metric.
func updateStatusMetrics() {
	resp, err := http.Get("http://localhost:26657/status")
	if err != nil {
		log.Println("Error fetching /status:", err)
		return
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		log.Println("Error decoding /status response:", err)
		return
	}

	// Extract server info.
	nodeID := status.Result.NodeInfo.ID
	network := status.Result.NodeInfo.Network
	version := status.Result.NodeInfo.Version

	// Set the metric value to 1 with the corresponding labels.
	serverInfo.WithLabelValues(nodeID, network, version).Set(1)

	// Update the peer count and peer version counts.
	peerCount.Set(float64(len(status.Result.Peers)))

	// Reset the peer version count metrics.
	peerVersionCount.Reset()

	// Count peers grouped by their version.
	peerVersionMap := make(map[string]int)
	for _, peer := range status.Result.Peers {
		peerVersionMap[peer.Version]++
	}

	for version, count := range peerVersionMap {
		peerVersionCount.WithLabelValues(version).Set(float64(count))
	}
}

// updateBlockMetrics fetches the latest block height and calculates block drift.
func updateBlockMetrics() {
	resp, err := http.Get("http://localhost:26657/status")
	if err != nil {
		log.Println("Error fetching /status:", err)
		return
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		log.Println("Error decoding /status response:", err)
		return
	}

	// Convert the block height to int64.
	height, err := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		log.Println("Error parsing block height:", err)
		return
	}

	// Process only new blocks.
	if height <= lastBlockHeight {
		return
	}

	// Update highest block number.
	highestBlock.Set(float64(height))

	// Convert block time to Go's time.Time format.
	blockTime, err := time.Parse(time.RFC3339, status.Result.SyncInfo.LatestBlockTime)
	if err != nil {
		log.Println("Error parsing block creation time:", err)
		return
	}

	// Calculate block drift.
	currentTime := time.Now()
	drift := currentTime.Sub(blockTime).Seconds()
	blockDrift.Set(drift)

	// Update last processed block height.
	lastBlockHeight = height
}

// scrapeMetrics calls our update functions.
func scrapeMetrics() {
	updateStatusMetrics()
	updateBlockMetrics()
}

func main() {
	// Start a goroutine that periodically scrapes metrics.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			scrapeMetrics()
			<-ticker.C
		}
	}()

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.Handler())
	log.Println("Starting exporter on :2112")
	log.Fatal(http.ListenAndServe(":2112", nil))
}
