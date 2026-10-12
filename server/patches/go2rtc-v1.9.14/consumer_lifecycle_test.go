package streams

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
)

type lifecycleProducer struct {
	core.Connection
	done  chan struct{}
	once  sync.Once
	stops atomic.Int32
}

func (p *lifecycleProducer) Start() error { <-p.done; return nil }
func (p *lifecycleProducer) Stop() error {
	p.stops.Add(1)
	_ = p.Connection.Stop()
	p.once.Do(func() { close(p.done) })
	return nil
}

type lifecycleViewer struct {
	core.Connection
	frames chan uint16
}

func (v *lifecycleViewer) AddTrack(media *core.Media, codec *core.Codec, track *core.Receiver) error {
	sender := core.NewSender(media, codec)
	sender.Handler = func(packet *core.Packet) { v.frames <- packet.SequenceNumber }
	sender.HandleRTP(track)
	v.Senders = append(v.Senders, sender)
	return nil
}
func expectFrame(t *testing.T, v *lifecycleViewer, sequence uint16) {
	t.Helper()
	select {
	case n := <-v.frames:
		if n != sequence {
			t.Fatalf("got frame%d want%d", n, sequence)
		}
	case <-time.After(time.Second):
		t.Fatalf("viewer did not receive frame%d", sequence)
	}
}
func TestRemoveDisconnectedViewerKeepsTwoHealthyViewersAndProducer(t *testing.T) {
	codec := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96}
	media := &core.Media{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{codec}}
	producer := &lifecycleProducer{Connection: core.Connection{Medias: []*core.Media{media}}, done: make(chan struct{})}
	stream := &Stream{producers: []*Producer{{conn: producer, state: stateMedias, url: "synthetic-shared-source"}}}
	viewers := make([]*lifecycleViewer, 3)
	for i := range viewers {
		viewers[i] = &lifecycleViewer{Connection: core.Connection{Medias: []*core.Media{{Kind: core.KindVideo, Direction: core.DirectionSendonly, Codecs: []*core.Codec{codec}}}}, frames: make(chan uint16, 32)}
		if err := stream.AddConsumer(viewers[i]); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, v := range viewers {
			_ = v.Stop()
		}
		if producer.stops.Load() == 0 {
			_ = producer.Stop()
		}
	}()
	receiver := producer.Receivers[0]
	receiver.WriteRTP(&core.Packet{Header: core.Packet{}.Header, Payload: []byte{1}})
	for _, v := range viewers {
		expectFrame(t, v, 0)
	}
	// The ordinary read-loop disconnect path removes one viewer, not the source.
	stream.RemoveConsumer(viewers[0])
	viewers[0].Senders[0].Wait()
	if len(stream.consumers) != 2 || producer.stops.Load() != 0 {
		t.Fatalf("disconnect affected shared producer: consumers=%d stops=%d", len(stream.consumers), producer.stops.Load())
	}
	for sequence := uint16(1); sequence <= 9; sequence++ {
		packet := new(core.Packet)
		packet.SequenceNumber = sequence
		packet.Payload = []byte{1}
		receiver.WriteRTP(packet)
		expectFrame(t, viewers[1], sequence)
		expectFrame(t, viewers[2], sequence)
	}
	select {
	case <-viewers[0].frames:
		t.Fatal("removed viewer received later frame")
	default:
	}
	stream.RemoveConsumer(viewers[1])
	viewers[1].Senders[0].Wait()
	if producer.stops.Load() != 0 {
		t.Fatal("producer stopped while one healthy viewer remained")
	}
	packet := new(core.Packet)
	packet.SequenceNumber = 10
	receiver.WriteRTP(packet)
	expectFrame(t, viewers[2], 10)
	stream.RemoveConsumer(viewers[2])
	viewers[2].Senders[0].Wait()
	if producer.stops.Load() != 1 {
		t.Fatalf("last viewer did not release producer: stops=%d", producer.stops.Load())
	}
}
