package remotelogging

import (
	"context"
	"log"
	"os"
	"sync"

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

func AddSinks(clients []pb.DiscoveryServiceClient) {
	for _, c := range clients {
		s := &sink{
			client: c,
			queue:  initBufferSink.queue,
		}
		go s.pusher()
		sinks = append(sinks, s)
	}
	if sinks[0] == initBufferSink {
		sinks = sinks[1:]
		initBufferSink.queue = nil
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
			_ = err
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
