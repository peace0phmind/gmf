package main

import (
	"flag"
	"github.com/peace0phmind/gmf"
	"log"
)

func main() {
	var (
		src string
		dst string
	)

	log.SetFlags(log.Lshortfile)
	gmf.LogSetLevel(gmf.AV_LOG_DEBUG)

	flag.StringVar(&src, "src", "rtsp://admin:Zyx123456@192.168.1.10", "source file")
	flag.StringVar(&dst, "dst", "test.mpeg", "destination file")
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

	outputCtx, err := gmf.NewOutputCtxWithFormatName(dst, "h264")
	if err != nil {
		log.Fatal(err)
	}
	defer outputCtx.Free()
	outputCtx.Dump()



}