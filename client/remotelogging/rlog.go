package remotelogging

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	pb "github.com/sgielen/rufs/proto"
)

var (
	mtx            sync.Mutex
	cond           = sync.NewCond(&mtx)
	initBufferSink = &sink{}
	sinks          = []*sink{
		initBufferSink,
	}
)

func init() {
	log.SetOutput(logWriter{})
}

type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	c := make([]byte, len(p))
	copy(c, p)
	mtx.Lock()
	for _, s := range sinks {
		s.Send(c)
	}
	cond.Broadcast()
	mtx.Unlock()
	return os.Stderr.Write(p)
}

func AddSink(client pb.DiscoveryServiceClient) {
	s := &sink{
		client: client,
		queue:  append([][]byte(nil), initBufferSink.queue...),
	}
	go s.pusher()
	mtx.Lock()
	defer mtx.Unlock()
	sinks = append(sinks, s)

	if sinks[0] == initBufferSink {
		// Allow other circles to connect within one second. After this,
		// clear the initial buffer sink.
		go func() {
			time.Sleep(time.Second)
			mtx.Lock()
			defer mtx.Unlock()
			if sinks[0] == initBufferSink {
				sinks = sinks[1:]
				initBufferSink.queue = nil
			}
		}()
	}
}

type sink struct {
	client pb.DiscoveryServiceClient
	queue  [][]byte
}

func (s *sink) Send(p []byte) {
	s.queue = append(s.queue, p)
}

func (s *sink) pusher() {
	ctx := context.Background()
	mtx.Lock()
	defer mtx.Unlock()
	for {
		for len(s.queue) > 0 {
			b := s.queue
			if len(b) > 100 {
				b = b[:100]
			}
			s.queue = s.queue[len(b):]
			mtx.Unlock()
			resp, err := s.client.PushLogs(ctx, &pb.PushLogsRequest{
				Messages: b,
			})
			if err != nil {
				os.Stderr.Write([]byte(fmt.Sprintf("Error uploading logs to discovery server: %v\n", err)))
			}
			mtx.Lock()
			if resp.GetStopSendingLogs() {
				s.queue = nil
				for i, s2 := range sinks {
					if s == s2 {
						sinks[i] = sinks[len(sinks)-1]
						sinks = sinks[:len(sinks)-1]
						break
					}
				}
				return
			}
		}
		cond.Wait()
	}
}
