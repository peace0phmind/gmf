package main

import (
	"errors"
	"flag"
	"github.com/peace0phmind/gmf"
	"io"
	"log"
	"runtime/debug"
	"syscall"

	//"syscall"
)

func fatal(err error) {
	debug.PrintStack()
	log.Fatal(err)
}

func assert(i interface{}, err error) interface{} {
	if err != nil {
		log.Fatal(err)
	}

	return i
}

func addStream(codecNameOrId interface{}, oc *gmf.FmtCtx, ist *gmf.Stream) (int, *gmf.Stream) {
	var cc *gmf.CodecCtx
	var ost *gmf.Stream

	codec := assert(gmf.FindEncoder(codecNameOrId)).(*gmf.Codec)

	if cc = gmf.NewCodecCtx(codec); cc == nil {
		log.Fatal(errors.New("unable to create codec context"))
	}

	if oc.IsGlobalHeader() {
		cc.SetFlag(gmf.CODEC_FLAG_GLOBAL_HEADER)
	}

	if codec.IsExperimental() {
		cc.SetStrictCompliance(gmf.FF_COMPLIANCE_EXPERIMENTAL)
	}

	if cc.Type() == gmf.AVMEDIA_TYPE_AUDIO {
		//cc.SetSampleFmt(ist.CodecCtx().SampleFmt())
		//cc.SetSampleRate(ist.CodecCtx().SampleRate())
		cc.SetChannels(ist.CodecCtx().Channels())
		cc.SelectChannelLayout()
		cc.SelectSampleRate()
	}

	if cc.Type() == gmf.AVMEDIA_TYPE_VIDEO {
		cc.SetDimension(1280, 720)
		cc.SetBitRate(1000)
		//cc.SetBitRate(5*1024*1024)
	}

	if ost = oc.NewStream(codec); ost == nil {
		log.Fatal(errors.New("unable to create stream in output context"))
	}

	ost.SetCodecCtx(cc)

	return ist.Index(), ost
}

func main() {
	var (
		src string
		dst string
	)

	log.SetFlags(log.Lshortfile)
	gmf.LogSetLevel(gmf.AV_LOG_DEBUG)

	flag.StringVar(&src, "src", "rtsp://admin:Zyx123456@192.168.1.10", "source file")
	//flag.StringVar(&dst, "dst", "http://121.36.218.177:9081/uVMChBVGg1", "destination file")
	flag.StringVar(&dst, "dst", "test.mpeg", "destination file")
	//flag.StringVar(&dst, "dst", "test.mp4", "destination file")
	flag.Parse()

	if len(src) == 0 || dst == "" {
		log.Fatal("at least one source and destination required, e.g.\n./watermark -src=bbb.mp4 -src=test.png -dst=overlay.mp4")
	}

	inputOptionsDict := gmf.NewDict([]gmf.Pair{{Key: "rtsp_transport", Val: "tcp"}, {Key: "stimeout", Val: "10000000"}})
	inputOption := &gmf.Option{Key: "input_options", Val: inputOptionsDict}
	inputCtx, err := gmf.NewInputCtxWithOption(src, inputOption)
	if err != nil {
		log.Fatal(err)
	}
	defer inputCtx.Free()
	inputCtx.Dump()

	outputCtx, err := gmf.NewOutputCtxWithFormatName(dst, "mpegts")
	if err != nil {
		log.Fatal(err)
	}
	defer outputCtx.Free()
	outputCtx.Dump()

	bestVideoStream := assert(inputCtx.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)).(*gmf.Stream)

	_, outVideoStream := addStream("mpeg1video", outputCtx, bestVideoStream)

	fg, err := gmf.NewVideoGraph("", []*gmf.Stream{bestVideoStream}, []*gmf.Stream{outVideoStream}, nil)
	defer fg.Release()
	fg.Dump()

	if err != nil {
		log.Fatalf("%s\n", err)
	}

	if err := outputCtx.WriteHeader(); err != nil {
		log.Fatalf("error writing header - %s\n", err)
	}

	init := false

	var (
		ret   int = 0
		frame *gmf.Frame
		ff    []*gmf.Frame
		pkt   *gmf.Packet
		ist   *gmf.Stream
		ost   *gmf.Stream
	)

	for {
		pkt, err = inputCtx.GetNextPacket()
		if err != nil && err != io.EOF {
			log.Fatalf("error getting next packet - %s", err)
		} else if err != nil && pkt == nil {
			log.Printf("EOF input, closing\n")
			fg.RequestOldest()
			fg.Close(0)
			continue
		}

		ist, err = inputCtx.GetStream(pkt.StreamIndex())
		if err != nil {
			log.Fatalf("%s\n", err)
		}

		if !ist.IsVideo() {
			continue
		}

		frame, ret = ist.CodecCtx().Decode2(pkt)
		if ret < 0 && gmf.AvErrno(ret) == syscall.EAGAIN {
			log.Printf("decode err")
			continue
		} else if ret == gmf.AVERROR_EOF {
			log.Fatalf("EOF in Decode2, handle it\n")
		} else if ret < 0 {
			log.Fatalf("Unexpected error - %s\n", gmf.AvError(ret))
		}

		//frame.SetPts(ist.Pts)
		//ist.Pts++

		if frame != nil && !init {
			if err := fg.AddFrame(frame, 0, 0); err != nil {
				log.Fatalf("%s\n", err)
			}
			fg.Dump()
			init = true
			frame.Free()
			continue
		}

		if err := fg.AddFrame(frame, 0, 4); err != nil {
			log.Fatalf("%s\n", err)
		}
		frame.Free()

		if ff, err = fg.GetFrame(); err != nil && len(ff) == 0 {
			//log.Printf("GetFrame() returned '%s', continue\n", err)
			continue
		}

		if len(ff) == 0 {
			continue
		}

		ost = assert(outputCtx.GetStream(pkt.StreamIndex())).(*gmf.Stream)
		packets, err := ost.CodecCtx().Encode(ff, -1)
		if err != nil {
			log.Fatalf("%s\n", err)
		}

		for _, f := range ff {
			f.Free()
		}

		for _, op := range packets {
			//gmf.RescaleTs(op, ost.CodecCtx().TimeBase(), ost.TimeBase())
			op.SetStreamIndex(ost.Index())

			op.Data()
			if err = outputCtx.WritePacket(op); err != nil {
				break
			}

			op.Free()
		}
	}

	outputCtx.WriteTrailer()

	ost.CodecCtx().Free()
	ost.Free()
}
