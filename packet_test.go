package gmf

import (
	"testing"
)

func TestFramesIterator(t *testing.T) {
	inputCtx, err := NewInputCtx(inputSampleFilename)
	if err != nil {
		t.Fatal(err)
	}
	defer inputCtx.Free()

	cnt := 0

	for packet := range inputCtx.GetNewPackets() {
		if packet.Size() <= 0 {
			t.Fatal("Expected size > 0")
		}

		ist := assert(inputCtx.GetStream(0)).(*Stream)

		frame, err := ist.CodecCtx().Decode(packet)
		if err != nil {
			t.Fatal(err)
		}
		if frame == nil {
			t.Fatal("Frame is nil")
		}

		cnt++

		packet.Free()
	}

	if cnt != 25 {
		t.Fatalf("Expected %d frames, obtained %d\n", 25, cnt)
	}
}
