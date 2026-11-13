package catalogeditor

import (
	"context"
	"testing"
)

type channelRecorder struct{ channels []string }

func (p *channelRecorder) Advance(_ context.Context, channel string, _ Request, _ Progress) (Result, error) {
	p.channels = append(p.channels, channel)
	return Result{Status: "queued"}, nil
}

func TestChannelPublishersNeverFallsThroughToAnotherWriteCapability(t *testing.T) {
	mp, saby := &channelRecorder{}, &channelRecorder{}
	providers := ChannelPublishers{"wb": mp, "ozon": mp, "saby": saby}
	for _, channel := range []string{"saby", "ozon", "wb"} {
		if _, err := providers.Advance(context.Background(), channel, Request{}, Progress{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(saby.channels) != 1 || saby.channels[0] != "saby" || len(mp.channels) != 2 {
		t.Fatal("write capabilities crossed channel boundaries")
	}
	if _, err := providers.Advance(context.Background(), "other", Request{}, Progress{}); err == nil {
		t.Fatal("unconfigured channel fell through to a live publisher")
	}
	if len(saby.channels) != 1 || len(mp.channels) != 2 {
		t.Fatal("unknown channel triggered a publisher")
	}
}
