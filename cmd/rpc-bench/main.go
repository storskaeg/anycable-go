package main

import (
	"flag"
	"os"
	"time"

	"github.com/anycable/anycable-go/config"
	"github.com/anycable/anycable-go/metrics"
	"github.com/anycable/anycable-go/single_rpc"

	log "github.com/apex/log"
)

var (
	runTime     int
	num         int
	testChannel = "{\"channel\":\"BenchmarkChannel\"}"
	testCommand = "{\"action\":\"echo\",\"text\":\"hello\"}"
	stats       *Stats
	controller  *single_rpc.Controller
)

type CallResult struct {
	ts    int64
	error error
}

type Stats struct {
	collect    chan *CallResult
	count      int64
	time       int64
	errors     int64
	shutdownCh chan bool
	done       chan bool
}

func NewStats() *Stats {
	return &Stats{
		collect:    make(chan *CallResult, 2048),
		count:      0,
		time:       0,
		errors:     0,
		shutdownCh: make(chan bool),
		done:       make(chan bool, 1),
	}
}

func (s *Stats) Start() {
	for {
		select {
		case call := <-s.collect:
			s.count++
			if call.error != nil {
				s.errors++
			}
			s.time += call.ts
		case <-s.shutdownCh:
			log.WithFields(log.Fields{
				"rps":    (s.count / int64(runTime)),
				"count":  s.count,
				"avg":    (s.time / int64(time.Microsecond)) / s.count,
				"errors": s.errors,
			}).Info("Results")
			s.done <- true
			break
		}
	}
}

func (s *Stats) Finish() {
	s.shutdownCh <- true
	<-s.done
}

func (s *Stats) Collect(res *CallResult) {
	s.collect <- res
}

func main() {
	config := config.New()

	flag.StringVar(&config.RPCHost, "h", "0.0.0.0:50051", "RPC host, default: 0.0.0.0:50051")
	flag.IntVar(&runTime, "t", 30, "Time to run benchmark (seconds), default: 30")
	flag.IntVar(&num, "c", 1, "Number of concurrent clients, default: 1")

	flag.Parse()

	metrics := metrics.NewMetrics(nil, 0)

	controller = single_rpc.NewController(&config, metrics)

	if err := controller.Start(); err != nil {
		log.Errorf("!!! RPC failed !!!\n%v", err)
		os.Exit(1)
	}

	stats = NewStats()

	go stats.Start()

	runClients()

	time.Sleep(5 * time.Second)

	log.Infof("Running %d clients for %ds", num, runTime)

	time.Sleep(time.Duration(runTime) * time.Second)

	log.Info("Stopping..")

	stats.Finish()
}

func runClients() {
	for i := 0; i < num; i++ {
		go runClient()

		if i%10 == 0 {
			time.Sleep(1 * time.Second)
		}
	}
}

func runClient() {
	headers := make(map[string]string)

	id, _, error := controller.Authenticate("/cable", &headers)

	if error != nil {
		log.Fatalf("Failed to authenticate client: %s", error)
	}

	_, error = controller.Subscribe("test", id, testChannel)

	if error != nil {
		log.Fatalf("Failed to subscribe client: %s", error)
	}

	for {
		start := time.Now()
		_, error = controller.Perform("test", id, testChannel, testCommand)
		t := time.Now()
		ts := t.Sub(start).Nanoseconds()

		stats.Collect(&CallResult{ts: ts, error: error})
	}
}
