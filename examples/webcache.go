package main

import (
	"errors"
	"flag"
	"github.com/peace0phmind/gmf"
	"io"
	"log"
	"runtime/debug"
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

func addStream(codecName string, oc *gmf.FmtCtx, ist *gmf.Stream) (int, int) {
	var cc *gmf.CodecCtx
	var ost *gmf.Stream

	codec := assert(gmf.FindEncoder(codecName)).(*gmf.Codec)

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
		cc.SetSampleFmt(ist.CodecCtx().SampleFmt())
		cc.SetSampleRate(ist.CodecCtx().SampleRate())
		cc.SetChannels(ist.CodecCtx().Channels())
		cc.SelectChannelLayout()
		cc.SelectSampleRate()

	}

	if cc.Type() == gmf.AVMEDIA_TYPE_VIDEO {
		cc.SetTimeBase(gmf.AVR{1, 25})
		cc.SetProfile(gmf.FF_PROFILE_MPEG4_SIMPLE)
		cc.SetDimension(ist.CodecCtx().Width(), ist.CodecCtx().Height())
		//cc.SetPixFmt(ist.CodecCtx().PixFmt())
		cc.SetPixFmt(gmf.AV_PIX_FMT_YUV420P)
		cc.SetBitRate(16e3)
	}

	if err := cc.Open(nil); err != nil {
		log.Fatal(err)
	}

	par := gmf.NewCodecParameters()
	if err := par.FromContext(cc); err != nil {
		log.Fatalf("error creating codec parameters from context - %s", err)
	}
	defer par.Free()

	if ost = oc.NewStream(codec); ost == nil {
		log.Fatal(errors.New("unable to create stream in output context"))
	}

	ost.CopyCodecPar(par)
	ost.SetCodecCtx(cc)
	ost.SetTimeBase(gmf.AVR{Num: 1, Den: 25})
	ost.SetRFrameRate(gmf.AVR{Num: 25, Den: 1})

	return ist.Index(), ost.Index()
}

func main() {
	var (
		src string
		dst string
	)

	log.SetFlags(log.Lshortfile)
	//gmf.LogSetLevel(gmf.AV_LOG_DEBUG)

	flag.StringVar(&src, "src", "rtsp://admin:Zyx123456@192.168.1.10", "source file")
	flag.StringVar(&dst, "dst", "http://121.36.218.177:9081/uVMChBVGg1", "destination file")
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

	var iVideoStream *gmf.Stream

	iVideoStream, err = inputCtx.GetBestStream(gmf.AVMEDIA_TYPE_VIDEO)
	if err != nil {
		log.Fatal(err)
	}
	iVideoStream.Free()

	addStream("mpeg1video", outputCtx, iVideoStream)
	log.Printf("stream %s, %s\n", iVideoStream.CodecCtx().Codec().LongName(), iVideoStream.CodecCtx().GetVideoSize())

	//filter, err := gmf.NewFilter("scale=size=1024x768", []*gmf.Stream{iVideoStream}, nil, nil)
	//defer filter.Release()
	//filter.Dump()

	if err != nil {
		log.Fatalf("%s\n", err)
	}

	if err := outputCtx.WriteHeader(); err != nil {
		log.Fatalf("error writing header - %s\n", err)
	}

	//init := false

	var (
		//ret   int = 0
		//frame *gmf.Frame
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
			//filter.RequestOldest()
			//filter.Close(0)
			continue
		}

		ist, err = inputCtx.GetStream(pkt.StreamIndex())
		if err != nil {
			log.Fatalf("%s\n", err)
		}

		if !ist.IsVideo() {
			continue
		}

		ff, err = ist.CodecCtx().Decode(pkt)
		//if ret < 0 && gmf.AvErrno(ret) == syscall.EAGAIN {
		//	continue
		//} else if ret == gmf.AVERROR_EOF {
		//	log.Fatalf("EOF in Decode2, handle it\n")
		//} else if ret < 0 {
		//	log.Fatalf("Unexpected error - %s\n", gmf.AvError(ret))
		//}

		//frame.SetPts(ist.Pts)
		//ist.Pts++

		//if frame != nil && !init {
		//	if err := filter.AddFrame(frame, 0, 0); err != nil {
		//		log.Fatalf("%s\n", err)
		//	}
		//	init = true
		//	continue
		//}

		//if err := filter.AddFrame(frame, 0, 4); err != nil {
		//	log.Fatalf("%s\n", err)
		//}
		//frame.Free()

		//if ff, err = filter.GetFrame(); err != nil && len(ff) == 0 {
		//	log.Printf("GetFrame() returned '%s', continue\n", err)
		//	continue
		//}

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
