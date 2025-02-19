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

//Ports used in this script:
//26657: For communicating with the Cosmos node to retrieve data.
//2112: For serving the metrics to Prometheus.

// Define Prometheus metrics.
// serverInfo is a GaugeVec that holds static information about the server.
var (
	serverInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "server_info",
			Help: "Static information about this Cosmos node (node ID, network and version).",
		},
		[]string{"node_id", "network", "version"},
	)

	txSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_success_total",
		Help: "Count of successful transactions",
	})
	txFail = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tx_fail_total",
		Help: "Count of failed transactions",
	})
)

func init() {
	prometheus.MustRegister(serverInfo)
	prometheus.MustRegister(txSuccess)
	prometheus.MustRegister(txFail)
}

// lastBlockHeight is used to ensure we process each block only once.
var lastBlockHeight int64 = 0

// StatusResponse models the JSON returned by the /status endpoint.
type StatusResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      string `json:"id"`
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
	} `json:"result"`
}

// BlockResultsResponse models the JSON returned by the /block_results endpoint.
type BlockResultsResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      string `json:"id"`
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
}

// updateTxMetrics queries the /block_results endpoint and updates transaction counters.
func updateTxMetrics() {
	resp, err := http.Get("http://localhost:26657/block_results")
	if err != nil {
		log.Println("Error fetching /block_results:", err)
		return
	}
	defer resp.Body.Close()

	var blockResults BlockResultsResponse
	if err := json.NewDecoder(resp.Body).Decode(&blockResults); err != nil {
		log.Println("Error decoding /block_results response:", err)
		return
	}

	// Convert the block height to int64.
	height, err := strconv.ParseInt(blockResults.Result.Height, 10, 64)
	if err != nil {
		log.Println("Error parsing block height:", err)
		return
	}

	// Process only new blocks.
	if height <= lastBlockHeight {
		return
	}

	// Increment txSuccess or txFail based on transaction result code.
	for _, txResult := range blockResults.Result.TxsResults {
		if txResult.Code == 0 {
			txSuccess.Inc()
		} else {
			txFail.Inc()
		}
	}

	lastBlockHeight = height
}

// scrapeMetrics calls our update functions.
func scrapeMetrics() {
	updateStatusMetrics()
	updateTxMetrics()
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
